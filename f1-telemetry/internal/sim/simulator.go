package sim

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Driver is the static description of one of the 20 entries.
type Driver struct {
	ID       int
	Code     string
	FullName string
	Team     string
	Number   int
	Color    string

	// PaceFactor is the per-driver multiplier on target speed: 1.00 is
	// average, 1.02 is a top-tier car, 0.95 is a backmarker.
	PaceFactor float64

	// CornerBias models a driver's preferred corner type. Positive values
	// mean the driver is faster than their baseline in HIGH-speed corners
	// (and proportionally slower in slow ones); negative is the opposite.
	// Range roughly ±0.006 (0.6% of pace).
	CornerBias float64
}

// DefaultGrid returns a 20-driver grid with fictional teams/codes so we
// don't impersonate real drivers.
//
// Pace model
//   - Each TEAM has a baseline pace, top to bottom: 1.020 .. 0.930.
//   - Within a team the two drivers are offset by ±0.005 around the team
//     mean (so a ~1% intra-team gap, roughly one second per lap on a 100-
//     second lap) — big enough that teammates visibly separate over 50
//     laps, small enough to stay realistic.
//   - On top of that, each driver picks up a small random "sprinkle"
//     (±0.0025) and a per-driver CornerBias (±0.006) so that some drivers
//     gain in fast sweepers while others gain in slow corners. Average
//     pace is preserved.
//
// The grid RNG is seeded with a fixed value so the demo is reproducible
// across runs (same fast/slow drivers each time); the per-tick sim noise
// (in advance()) still uses a wall-clock seed for variety.
func DefaultGrid() []Driver {
	type teamSpec struct {
		name  string
		color string
		pace  float64
	}
	teams := []teamSpec{
		{"Apex Racing", "#1ee2a2", 1.020},
		{"Velocity GP", "#ff5577", 1.010},
		{"Crimson Wing", "#cc2222", 1.000},
		{"Solaris", "#ffaa00", 0.990},
		{"Nordic", "#3aa6ff", 0.980},
		{"Vortex", "#9966ff", 0.970},
		{"Iron Lotus", "#22cc88", 0.960},
		{"Meridian", "#bbbbbb", 0.950},
		{"Polestar Works", "#ff8c1a", 0.940},
		{"Sable", "#888888", 0.930},
	}
	codes := []string{
		"ANV", "BRK", "CRO", "DEL", "EZK", "FAR", "GRV", "HIN", "ILA", "JEN",
		"KOR", "LIA", "MUR", "NEV", "ORS", "PAX", "QIN", "ROS", "SAV", "TOR",
	}
	names := []string{
		"A. Anvers", "B. Brokk", "C. Cross", "D. Delacroix", "E. Ezakov",
		"F. Faruq", "G. Greaves", "H. Hinata", "I. Ilanov", "J. Jensen",
		"K. Korva", "L. Liang", "M. Murat", "N. Neves", "O. Orsini",
		"P. Paxson", "Q. Quintero", "R. Rosso", "S. Savic", "T. Torres",
	}

	gridRNG := rand.New(rand.NewSource(0xCEDA12B))
	out := make([]Driver, 20)
	for i := 0; i < 20; i++ {
		team := teams[i/2]

		// Intra-team: the #1 driver is 0.005 faster than the team mean,
		// the #2 driver 0.005 slower. ~1% gap, ~1s/lap.
		intraTeam := 0.005
		if i%2 == 1 {
			intraTeam = -0.005
		}
		// Small extra sprinkle so the order isn't perfectly tidy.
		sprinkle := (gridRNG.Float64()*2 - 1) * 0.0025

		// Corner specialty: zero-mean across drivers. A driver with +0.006
		// is ~0.6% faster than baseline on full straights / fast sweepers
		// and ~0.6% slower in slow corners; the opposite for negative.
		cornerBias := (gridRNG.Float64()*2 - 1) * 0.006

		out[i] = Driver{
			ID:         i + 1,
			Code:       codes[i],
			FullName:   names[i],
			Team:       team.name,
			Number:     10 + i,
			Color:      team.color,
			PaceFactor: team.pace + intraTeam + sprinkle,
			CornerBias: cornerBias,
		}
	}
	return out
}

// state holds the per-driver mutable state during simulation.
type state struct {
	driver      Driver
	trackPos    float64 // 0..1
	speedKph    float64
	lap         int
	lapStart    time.Time
	sectorStart time.Time
	sector      int    // 0,1,2
	sectorTimes [3]int // ms
	bestLap     int    // ms
	finished    bool

	// formDelta is a slow-varying per-driver pace bonus that random-walks
	// during the race. It represents the driver's "form" / tyre state /
	// fuel mass / general luck-of-the-stint. Bounded to ±0.008 so it never
	// dominates the static PaceFactor difference between teams.
	formDelta float64
}

// Simulator drives 20 cars around a Track and inserts both telemetry samples
// and completed laps into CedarDB.
type Simulator struct {
	Pool      *pgxpool.Pool
	Track     *Track
	Drivers   []Driver
	SessionID int64
	TotalLaps int
	TickHz    int

	state []state
	rng   *rand.Rand
}

// NewSimulator creates a session row, seeds drivers, and prepares per-car
// state on a starting grid. It does NOT begin the loop -- call Run for that.
func NewSimulator(ctx context.Context, pool *pgxpool.Pool, track *Track, drivers []Driver, totalLaps, tickHz int) (*Simulator, error) {
	var sessionID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO sessions (name, track, total_laps)
		 VALUES ($1, $2, $3) RETURNING session_id`,
		"Live Demo Race", track.Name, totalLaps,
	).Scan(&sessionID)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Upsert drivers (idempotent across runs).
	batch := &pgx.Batch{}
	for _, d := range drivers {
		batch.Queue(
			`INSERT INTO drivers (driver_id, code, full_name, team, car_number, color_hex)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (driver_id) DO UPDATE SET
			   code=EXCLUDED.code, full_name=EXCLUDED.full_name,
			   team=EXCLUDED.team, car_number=EXCLUDED.car_number,
			   color_hex=EXCLUDED.color_hex`,
			d.ID, d.Code, d.FullName, d.Team, d.Number, d.Color)
	}
	if err := pool.SendBatch(ctx, batch).Close(); err != nil {
		return nil, fmt.Errorf("seed drivers: %w", err)
	}

	// Starting grid: small backward offset per car so they don't all stack at
	// the start line. Position is in track_pos units (fraction of a lap).
	now := time.Now()
	st := make([]state, len(drivers))
	for i, d := range drivers {
		st[i] = state{
			driver:      d,
			trackPos:    -0.001 * float64(i), // wraps to ~0.999 etc.
			speedKph:    0,
			lap:         1,
			lapStart:    now,
			sectorStart: now,
		}
	}

	return &Simulator{
		Pool:      pool,
		Track:     track,
		Drivers:   drivers,
		SessionID: sessionID,
		TotalLaps: totalLaps,
		TickHz:    tickHz,
		state:     st,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

// Run advances the simulation at TickHz until all cars finish TotalLaps or
// the context is cancelled. Each tick produces one telemetry row per car;
// laps are inserted whenever a car crosses the start/finish line.
func (s *Simulator) Run(ctx context.Context) error {
	// Insert a 'race_start' event for the timeline.
	_, _ = s.Pool.Exec(ctx,
		`INSERT INTO events (session_id, ts, kind, detail) VALUES ($1, now(), 'race_start', $2)`,
		s.SessionID, fmt.Sprintf("%d cars, %d laps", len(s.Drivers), s.TotalLaps))

	interval := time.Second / time.Duration(s.TickHz)
	tick := time.NewTicker(interval)
	defer tick.Stop()

	dt := 1.0 / float64(s.TickHz) // seconds per tick

	// Pre-allocate the batch we send each tick.
	tele := make([][]any, 0, len(s.Drivers))

	for {
		select {
		case <-ctx.Done():
			s.endSession(context.Background())
			return ctx.Err()
		case now := <-tick.C:
			tele = tele[:0]
			allDone := true

			for i := range s.state {
				cs := &s.state[i]
				if cs.finished {
					continue
				}
				allDone = false
				s.advance(cs, dt)

				p, _ := s.Track.PosAt(cs.trackPos)
				gear, rpm, throttle, brake, drs := derive(cs.speedKph, cs.trackPos, s.Track)

				tele = append(tele, []any{
					s.SessionID, cs.driver.ID, now, cs.lap,
					cs.trackPos, p.X, p.Y, cs.speedKph,
					rpm, gear, throttle, brake, drs,
				})

				// Check sector / lap crossings.
				s.handleSectors(ctx, cs, now)
			}

			if len(tele) > 0 {
				_, err := s.Pool.CopyFrom(ctx,
					pgx.Identifier{"telemetry"},
					[]string{"session_id", "driver_id", "ts", "lap",
						"track_pos", "pos_x", "pos_y", "speed_kph",
						"rpm", "gear", "throttle", "brake", "drs"},
					pgx.CopyFromRows(tele),
				)
				if err != nil {
					return fmt.Errorf("copy telemetry: %w", err)
				}
			}

			if allDone {
				s.endSession(ctx)
				return nil
			}
		}
	}
}

// advance moves a single car forward by dt seconds.
//
// Pace is composed each tick from four pieces so that 20 cars with the same
// underlying physics actually finish in different positions:
//
//	final_pace = driver.PaceFactor          // static team + intra-team rank
//	           + driver.CornerBias * mix    // dynamic, depends on corner type
//	           + state.formDelta            // slow random walk, lap-scale drift
//	           + ε   (1.5 km/h jitter)      // sub-corner noise
//
// `mix` is 0 in a 90 km/h hairpin and 1 on a 320 km/h straight, so the bias
// term reverses sign smoothly across the lap.
func (s *Simulator) advance(cs *state, dt float64) {
	baseTarget := s.Track.TargetSpeedAt(cs.trackPos)

	// Corner-type weighting: 0 = slow corner, 1 = fast straight.
	const slowKph = 110.0
	const fastKph = 290.0
	mix := (baseTarget - slowKph) / (fastKph - slowKph)
	if mix < 0 {
		mix = 0
	}
	if mix > 1 {
		mix = 1
	}
	// Positive CornerBias adds in fast sections, subtracts in slow ones.
	cornerAdj := cs.driver.CornerBias * (2*mix - 1)

	// Slow random walk on form. ±1e-4 per tick, bounded to ±0.008. At 10 Hz
	// this drifts on the order of 0.001/sec, so it takes a couple of laps
	// to traverse the full range — i.e. lap-scale drift, not noise.
	cs.formDelta += (s.rng.Float64()*2 - 1) * 1e-4
	if cs.formDelta > 0.008 {
		cs.formDelta = 0.008
	} else if cs.formDelta < -0.008 {
		cs.formDelta = -0.008
	}

	pace := cs.driver.PaceFactor + cornerAdj + cs.formDelta
	target := baseTarget * pace
	// Tiny noise so cars don't move in lockstep within a corner.
	target += (s.rng.Float64()*2 - 1) * 1.5

	// Accelerate / brake toward target. Real F1 cars hit ~3-5g braking and
	// ~1-2g accel; we pick numbers that look right at 10Hz.
	const accelKphPerSec = 95.0
	const brakeKphPerSec = 230.0
	delta := target - cs.speedKph
	maxStep := accelKphPerSec * dt
	if delta < 0 {
		maxStep = brakeKphPerSec * dt
	}
	if math.Abs(delta) <= maxStep {
		cs.speedKph = target
	} else if delta > 0 {
		cs.speedKph += maxStep
	} else {
		cs.speedKph -= maxStep
	}
	if cs.speedKph < 30 {
		cs.speedKph = 30 // never crawl: avoids div-by-zero everywhere
	}

	// Move along the track. trackPos is a fraction of a lap [0,1), so
	// delta-pos = (meters traveled this tick) / (lap length in meters).
	mps := cs.speedKph * 1000.0 / 3600.0
	cs.trackPos += (mps * dt) / s.Track.LapMeters
}

// derive computes RPM, gear, throttle, brake, DRS from speed + track context.
// These are derived (not independently simulated) but consistent.
func derive(speedKph, trackPos float64, t *Track) (gear int16, rpm int, throttle, brake float32, drs bool) {
	// Gear from speed bands.
	switch {
	case speedKph < 80:
		gear = 2
	case speedKph < 130:
		gear = 3
	case speedKph < 180:
		gear = 4
	case speedKph < 230:
		gear = 5
	case speedKph < 270:
		gear = 6
	case speedKph < 305:
		gear = 7
	default:
		gear = 8
	}
	// RPM rises within each gear, snaps down on shift. Roughly 8000-12000.
	gearBase := []int{0, 7000, 8000, 8500, 9000, 9500, 10000, 10500, 11000}
	rpm = gearBase[gear] + int(((speedKph-float64((int(gear)-1)*45))/45.0)*1500)
	if rpm < 6500 {
		rpm = 6500
	}
	if rpm > 12000 {
		rpm = 12000
	}

	// Throttle / brake based on speed vs the local target. Anticipatory: if the
	// target ahead is much lower, we must be braking; if higher, on the gas.
	target := t.TargetSpeedAt(trackPos)
	switch {
	case target < speedKph-15:
		brake = float32(math.Min(1.0, (speedKph-target)/80.0))
		throttle = 0
	case target > speedKph+5:
		throttle = 1
		brake = 0
	default:
		throttle = 0.6
		brake = 0
	}

	// DRS open on long fast sections (rough proxy: target speed > 290).
	drs = target > 290 && speedKph > 250
	return
}

// handleSectors detects sector crossings and lap completion.
func (s *Simulator) handleSectors(ctx context.Context, cs *state, now time.Time) {
	// Sector boundaries at 1/3, 2/3, 1.0.
	boundaries := []float64{1.0 / 3.0, 2.0 / 3.0, 1.0}
	for cs.sector < 3 && cs.trackPos >= boundaries[cs.sector] {
		elapsed := int(now.Sub(cs.sectorStart) / time.Millisecond)
		cs.sectorTimes[cs.sector] = elapsed
		cs.sector++
		cs.sectorStart = now
		if cs.sector == 3 {
			// Lap complete.
			lapMs := cs.sectorTimes[0] + cs.sectorTimes[1] + cs.sectorTimes[2]
			s.insertLap(ctx, cs, lapMs, now)
			cs.lap++
			cs.lapStart = now
			cs.sector = 0
			cs.sectorTimes = [3]int{}
			cs.trackPos -= 1.0 // wrap
			if cs.bestLap == 0 || lapMs < cs.bestLap {
				cs.bestLap = lapMs
			}
			if cs.lap > s.TotalLaps {
				cs.finished = true
			}
		}
	}
}

func (s *Simulator) insertLap(ctx context.Context, cs *state, lapMs int, finishedAt time.Time) {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO laps (session_id, driver_id, lap_number, lap_time_ms,
		                   sector1_ms, sector2_ms, sector3_ms, finished_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT DO NOTHING`,
		s.SessionID, cs.driver.ID, cs.lap, lapMs,
		cs.sectorTimes[0], cs.sectorTimes[1], cs.sectorTimes[2], finishedAt,
	)
	if err != nil {
		// Non-fatal: log and continue. The simulation should not stop because
		// one INSERT failed.
		fmt.Printf("insert lap: %v\n", err)
	}
}

func (s *Simulator) endSession(ctx context.Context) {
	_, _ = s.Pool.Exec(ctx,
		`UPDATE sessions SET ended_at = now() WHERE session_id = $1`,
		s.SessionID)
}
