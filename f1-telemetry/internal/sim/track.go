package sim

import (
	"math"
	"strings"
)

// Track is a closed loop of waypoints. Each waypoint carries a target speed
// that the cars try to drive at while passing through it. By giving curvy
// regions a low target speed and straights a high one, we get realistic-looking
// throttle/brake/gear behavior without simulating actual vehicle dynamics.
type Track struct {
	Name      string
	Points    []Point   // closed loop, Points[0] == start/finish line
	TargetKph []float64 // len == len(Points), target speed at each waypoint

	// Length is the geometric arc length of the SVG polyline, in the same
	// arbitrary units as Points. Used for proportional lookup along the
	// closed curve (see PosAt), nothing else.
	Length float64

	// LapMeters is the physical lap length used by the simulator to convert
	// km/h into "fraction of lap per tick". Decoupling this from Length means
	// we can pick a realistic F1 lap length (~5.4 km) without having to scale
	// the SVG geometry up.
	LapMeters float64

	cumLen []float64 // cumulative arc length, for track_pos lookup
}

type Point struct {
	X, Y float64
}

// Preset describes one of the selectable track variants. Add new tracks by
// appending to the Presets slice — make sure the control points form a clean
// counter-clockwise loop with no self-intersections.
type Preset struct {
	Key       string  // machine key (used by the simulator's -track flag)
	Display   string  // human name; written into sessions.track
	LapMeters float64 // physical lap length used by the simulator
	Tagline   string  // one-line description shown by -list-tracks
	Controls  []Point // hand-picked spline control points (CCW)
}

// Presets are the 6 selectable circuits. They're deliberately drawn to look
// obviously different from each other so it's easy to tell which one is up.
var Presets = []Preset{
	{
		Key: "cedar-park", Display: "Cedar Park Circuit", LapMeters: 5400,
		Tagline: "Balanced — long straight, fast right-hander, chicane, hairpin",
		Controls: []Point{
			{160, 600}, {310, 600}, {480, 595}, {650, 590},
			{790, 565}, {860, 510}, {885, 420}, {870, 330},
			{790, 265}, {680, 230}, {570, 200}, {495, 230}, {420, 200},
			{310, 215}, {210, 260}, {155, 335},
			{130, 425}, {150, 520}, {135, 580},
		},
	},
	{
		Key: "sprint-oval", Display: "Sprint Oval", LapMeters: 4200,
		Tagline: "High-speed — long straights, two big sweepers, no real corners",
		Controls: []Point{
			{160, 380}, {200, 250}, {340, 175}, {660, 175},
			{800, 250}, {840, 380}, {800, 510}, {660, 585},
			{340, 585}, {200, 510},
		},
	},
	{
		Key: "highline", Display: "Highline Esses", LapMeters: 5200,
		Tagline: "Flowing — shallow sweepers across a long horizontal band",
		Controls: []Point{
			{140, 470}, {300, 410}, {470, 460}, {640, 400},
			{800, 450}, {875, 510}, {820, 570}, {660, 585},
			{480, 580}, {300, 575}, {170, 555},
		},
	},
	{
		Key: "crescent", Display: "Crescent Bay", LapMeters: 5100,
		Tagline: "Asymmetric arc with a tight infield twist on the south side",
		Controls: []Point{
			{140, 540}, {310, 590}, {490, 595}, {660, 580},
			{790, 520}, {855, 410}, {820, 290}, {700, 220},
			{570, 230}, {490, 310}, {420, 360}, {340, 320},
			{240, 360}, {170, 450},
		},
	},
	{
		Key: "pinewood", Display: "Pinewood Climb", LapMeters: 4800,
		Tagline: "Vertical layout — two opposed hairpins, narrow infield",
		Controls: []Point{
			{420, 620}, {510, 600}, {590, 560}, {635, 480},
			{590, 410}, {510, 380}, {460, 330}, {480, 250},
			{560, 190}, {660, 170}, {740, 220}, {750, 320},
			{700, 400}, {640, 460}, {640, 540}, {560, 590},
		},
	},
	{
		Key: "downtown", Display: "Downtown Loop", LapMeters: 4400,
		Tagline: "Street circuit — boxy silhouette, right-angle turns, slow average",
		Controls: []Point{
			{180, 580}, {380, 590}, {560, 585}, {700, 565},
			{760, 510}, {760, 430}, {700, 410}, {640, 430},
			{640, 320}, {730, 290}, {730, 200}, {560, 190},
			{390, 195}, {300, 240}, {290, 340}, {240, 380},
			{240, 460}, {190, 490},
		},
	},
}

// PresetByName returns the preset whose Key or Display matches (case- and
// whitespace-insensitive). Falls back to Presets[0] (Cedar Park) if no
// match — that's intentional so a typo at the CLI gives you a working race
// rather than a panic.
func PresetByName(name string) Preset {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range Presets {
		if strings.ToLower(p.Key) == n || strings.ToLower(p.Display) == n {
			return p
		}
	}
	return Presets[0]
}

// GenerateTrack builds the named preset by interpolating its control points
// with a centripetal Catmull-Rom spline. nameOrKey may be either a preset's
// Key (e.g. "cedar-park") or its Display name (e.g. "Cedar Park Circuit");
// unknown names fall back to the first preset.
//
// nPoints is a hint for the desired number of sampled waypoints; it's
// rounded down to a clean number of samples per control segment.
func GenerateTrack(nameOrKey string, nPoints int) *Track {
	p := PresetByName(nameOrKey)
	if nPoints < 100 {
		nPoints = 240
	}
	samplesPerSeg := nPoints / len(p.Controls)
	if samplesPerSeg < 8 {
		samplesPerSeg = 8
	}
	pts := closedCatmullRom(p.Controls, samplesPerSeg)
	n := len(pts)

	t := &Track{
		Name:      p.Display,
		Points:    pts,
		TargetKph: make([]float64, n),
		cumLen:    make([]float64, n),
		LapMeters: p.LapMeters,
	}

	// Cumulative arc length, then total length (closing the loop).
	for i := 1; i < n; i++ {
		dx := t.Points[i].X - t.Points[i-1].X
		dy := t.Points[i].Y - t.Points[i-1].Y
		t.cumLen[i] = t.cumLen[i-1] + math.Hypot(dx, dy)
	}
	closeDx := t.Points[0].X - t.Points[n-1].X
	closeDy := t.Points[0].Y - t.Points[n-1].Y
	t.Length = t.cumLen[n-1] + math.Hypot(closeDx, closeDy)

	// Target speed at each waypoint, based on local curvature. Tight corners
	// get low target speeds; straight sections get high ones. Curvature is
	// approximated from the angle change across a small window scaled to the
	// point density.
	window := n / 40
	if window < 4 {
		window = 4
	}
	for i := 0; i < n; i++ {
		a := (i - window + n) % n
		b := (i + window) % n
		v1x := t.Points[i].X - t.Points[a].X
		v1y := t.Points[i].Y - t.Points[a].Y
		v2x := t.Points[b].X - t.Points[i].X
		v2y := t.Points[b].Y - t.Points[i].Y
		ang := math.Atan2(v1x*v2y-v1y*v2x, v1x*v2x+v1y*v2y)
		curve := math.Abs(ang)

		const maxKph = 325.0
		const minKph = 85.0
		kph := maxKph - (maxKph-minKph)*math.Min(1.0, curve/0.45)
		t.TargetKph[i] = kph
	}

	// Smooth the target-speed profile so braking zones extend back from the
	// apex (cars start slowing before the corner).
	for pass := 0; pass < 6; pass++ {
		out := make([]float64, n)
		for i := range t.TargetKph {
			p := t.TargetKph[(i-1+n)%n]
			c := t.TargetKph[i]
			nx := t.TargetKph[(i+1)%n]
			blend := 0.25*p + 0.5*c + 0.25*nx
			out[i] = math.Min(blend, c+10)
		}
		t.TargetKph = out
	}

	return t
}

// PosAt returns the (x, y) point and the index of the waypoint just before
// the given track_pos in [0, 1). It interpolates linearly between waypoints.
func (t *Track) PosAt(pos float64) (Point, int) {
	pos = pos - math.Floor(pos) // wrap to [0, 1)
	target := pos * t.Length

	lo, hi := 0, len(t.cumLen)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if t.cumLen[mid] <= target {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	i := lo
	next := (i + 1) % len(t.Points)
	segStart := t.cumLen[i]
	var segLen float64
	if next == 0 {
		segLen = t.Length - segStart
	} else {
		segLen = t.cumLen[next] - segStart
	}
	frac := 0.0
	if segLen > 0 {
		frac = (target - segStart) / segLen
	}
	p := Point{
		X: t.Points[i].X + frac*(t.Points[next].X-t.Points[i].X),
		Y: t.Points[i].Y + frac*(t.Points[next].Y-t.Points[i].Y),
	}
	return p, i
}

// TargetSpeedAt returns the target km/h at the given track position, with a
// short look-ahead so cars start braking BEFORE the corner.
func (t *Track) TargetSpeedAt(pos float64) float64 {
	const lookAhead = 0.02
	_, i := t.PosAt(pos + lookAhead)
	_, j := t.PosAt(pos + 2*lookAhead)
	a := t.TargetKph[i]
	b := t.TargetKph[j]
	if b < a {
		return b
	}
	return a
}

// closedCatmullRom interpolates a closed loop of control points using a
// centripetal Catmull-Rom spline.
func closedCatmullRom(ctrl []Point, samplesPerSeg int) []Point {
	n := len(ctrl)
	out := make([]Point, 0, n*samplesPerSeg)
	for i := 0; i < n; i++ {
		p0 := ctrl[(i-1+n)%n]
		p1 := ctrl[i]
		p2 := ctrl[(i+1)%n]
		p3 := ctrl[(i+2)%n]

		t0 := 0.0
		t1 := t0 + math.Sqrt(pointDist(p0, p1))
		t2 := t1 + math.Sqrt(pointDist(p1, p2))
		t3 := t2 + math.Sqrt(pointDist(p2, p3))
		if t1 <= t0 {
			t1 = t0 + 1e-6
		}
		if t2 <= t1 {
			t2 = t1 + 1e-6
		}
		if t3 <= t2 {
			t3 = t2 + 1e-6
		}

		for s := 0; s < samplesPerSeg; s++ {
			t := t1 + (t2-t1)*float64(s)/float64(samplesPerSeg)
			out = append(out, catmullRomEval(p0, p1, p2, p3, t0, t1, t2, t3, t))
		}
	}
	return out
}

// catmullRomEval evaluates the centripetal Catmull-Rom spline at parameter t,
// using the Barry-Goldman pyramid (three lerp levels).
func catmullRomEval(p0, p1, p2, p3 Point, t0, t1, t2, t3, t float64) Point {
	a1 := lerpPoint(p0, p1, (t-t0)/(t1-t0))
	a2 := lerpPoint(p1, p2, (t-t1)/(t2-t1))
	a3 := lerpPoint(p2, p3, (t-t2)/(t3-t2))
	b1 := lerpPoint(a1, a2, (t-t0)/(t2-t0))
	b2 := lerpPoint(a2, a3, (t-t1)/(t3-t1))
	return lerpPoint(b1, b2, (t-t1)/(t2-t1))
}

func lerpPoint(a, b Point, t float64) Point {
	return Point{X: a.X + t*(b.X-a.X), Y: a.Y + t*(b.Y-a.Y)}
}

func pointDist(a, b Point) float64 {
	return math.Hypot(a.X-b.X, a.Y-b.Y)
}
