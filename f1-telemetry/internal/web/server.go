package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/cedardb-demo/f1-telemetry/internal/sim"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed templates/*.html
var templatesFS embed.FS

// staticFS holds files served under /static/. Drop replacement assets
// (e.g. the real cedardb-logo.svg or a PNG variant) into internal/web/static
// and rebuild — they'll be picked up automatically.
//
//go:embed static/*
var staticFS embed.FS

// Server holds the HTTP routes and CedarDB pool.
type Server struct {
	Pool   *pgxpool.Pool
	tpl    *template.Template
	static http.Handler
}

func NewServer(pool *pgxpool.Pool) (*Server, error) {
	tpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("static sub: %w", err)
	}
	return &Server{
		Pool:   pool,
		tpl:    tpl,
		static: http.FileServer(http.FS(sub)),
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/track.json", s.handleTrackJSON)
	mux.HandleFunc("/sse/state", s.handleSSE)
	mux.HandleFunc("/api/leaderboard", s.handleLeaderboard)
	mux.HandleFunc("/api/speed-map", s.handleSpeedMap)
	mux.HandleFunc("/api/ingest-rate", s.handleIngestRate)
	mux.Handle("/static/", http.StripPrefix("/static/", s.static))
	return mux
}

// --- Models the SSE payload uses -----------------------------------------

type carState struct {
	DriverID  int     `json:"driver_id"`
	Code      string  `json:"code"`
	Team      string  `json:"team"`
	Color     string  `json:"color"`
	Lap       int     `json:"lap"`
	PosX      float64 `json:"x"`
	PosY      float64 `json:"y"`
	SpeedKph  float64 `json:"speed_kph"`
	RPM       int     `json:"rpm"`
	Gear      int16   `json:"gear"`
	Throttle  float32 `json:"throttle"`
	Brake     float32 `json:"brake"`
	DRS       bool    `json:"drs"`
	BestLapMs int     `json:"best_lap_ms"`
	LastLapMs int     `json:"last_lap_ms"`
}

type sessionInfo struct {
	SessionID int64  `json:"session_id"`
	Name      string `json:"name"`
	Track     string `json:"track"`
	TotalLaps int    `json:"total_laps"`
}

type stateSnapshot struct {
	Session sessionInfo `json:"session"`
	Cars    []carState  `json:"cars"`
	StampMs int64       `json:"stamp_ms"`
}

// --- Handlers -------------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if err := s.tpl.ExecuteTemplate(w, "index.html", nil); err != nil {
		log.Printf("template: %v", err)
	}
}

// handleTrackJSON returns the track polyline plus finish-line geometry so
// the client can render the SVG once per session. The track is stored in
// the DB only as session.track (display name); the geometry is regenerated
// deterministically server-side from the matching preset in
// internal/sim/track.go.
//
// The finish_line field contains the start/finish point and a unit normal
// vector pointing perpendicular to the racing line at that point, so the
// client can rotate a checkered marker into place without having to figure
// out the tangent itself.
func (s *Server) handleTrackJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var trackName string
	err := s.Pool.QueryRow(ctx, `
		SELECT track FROM sessions
		ORDER BY (ended_at IS NULL) DESC, started_at DESC
		LIMIT 1
	`).Scan(&trackName)
	if err != nil {
		// No session yet — fall back to the default preset so the page
		// still loads with something visible.
		trackName = sim.Presets[0].Display
	}
	t := sim.GenerateTrack(trackName, 240)

	pts := make([][2]float64, len(t.Points))
	for i, p := range t.Points {
		pts[i] = [2]float64{p.X, p.Y}
	}

	// Unit normal to the racing line at trackPos = 0, pointing "left" of the
	// direction of travel. Used by the client to orient the checkered band.
	p0 := t.Points[0]
	p1 := t.Points[1%len(t.Points)]
	dx := p1.X - p0.X
	dy := p1.Y - p0.Y
	norm := math.Hypot(dx, dy)
	if norm == 0 {
		norm = 1
	}
	nx, ny := -dy/norm, dx/norm

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"points":     pts,
		"track_name": t.Name,
		"finish_line": map[string]float64{
			"x":  p0.X,
			"y":  p0.Y,
			"nx": nx,
			"ny": ny,
		},
	})
}

// handleSSE streams a JSON snapshot of the live race state every 200ms.
//
// This is the panel that demonstrates "concurrent ingest + analytic reads on
// the same table." Each tick we run a window-function query over `telemetry`
// to pick the latest row per driver while the simulator is still INSERTing.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()

	// Initial push.
	if err := s.pushSnapshot(ctx, w, flusher); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.pushSnapshot(ctx, w, flusher); err != nil {
				return
			}
		}
	}
}

func (s *Server) pushSnapshot(ctx context.Context, w http.ResponseWriter, f http.Flusher) error {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		// Skip this tick on error but keep the stream open.
		log.Printf("snapshot: %v", err)
		return nil
	}
	buf, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", buf); err != nil {
		return err
	}
	f.Flush()
	return nil
}

// loadSnapshot picks the live session and runs one query that returns the
// most recent telemetry row per driver, joined with driver metadata, plus
// their best / last lap times.
//
// Query notes:
//   - DISTINCT ON (driver_id) ... ORDER BY driver_id, ts DESC matches the
//     telemetry_session_driver_ts_idx index.
//   - The laps lookup is cheap because the table is small relative to telemetry.
//
// CedarDB executes both reads concurrently with the simulator's INSERTs,
// which is the differentiator we want to showcase.
func (s *Server) loadSnapshot(ctx context.Context) (stateSnapshot, error) {
	// Latest live (not-yet-ended) session, falling back to most recent.
	var sess sessionInfo
	err := s.Pool.QueryRow(ctx, `
		SELECT session_id, name, track, total_laps
		FROM sessions
		ORDER BY (ended_at IS NULL) DESC, started_at DESC
		LIMIT 1
	`).Scan(&sess.SessionID, &sess.Name, &sess.Track, &sess.TotalLaps)
	if err != nil {
		return stateSnapshot{}, err
	}

	rows, err := s.Pool.Query(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (driver_id)
			       driver_id, ts, lap, pos_x, pos_y, speed_kph,
			       rpm, gear, throttle, brake, drs
			FROM telemetry
			WHERE session_id = $1
			ORDER BY driver_id, ts DESC
		),
		lap_stats AS (
			SELECT driver_id,
			       MIN(lap_time_ms)                                       AS best_lap_ms,
			       (ARRAY_AGG(lap_time_ms ORDER BY lap_number DESC))[1]   AS last_lap_ms
			FROM laps
			WHERE session_id = $1
			GROUP BY driver_id
		)
		SELECT d.driver_id, d.code, d.team, d.color_hex,
		       l.lap, l.pos_x, l.pos_y, l.speed_kph,
		       l.rpm, l.gear, l.throttle, l.brake, l.drs,
		       COALESCE(s.best_lap_ms, 0), COALESCE(s.last_lap_ms, 0)
		FROM   latest l
		JOIN   drivers d ON d.driver_id = l.driver_id
		LEFT JOIN lap_stats s ON s.driver_id = l.driver_id
		ORDER BY d.driver_id
	`, sess.SessionID)
	if err != nil {
		return stateSnapshot{}, err
	}
	defer rows.Close()

	cars := make([]carState, 0, 20)
	for rows.Next() {
		var c carState
		if err := rows.Scan(
			&c.DriverID, &c.Code, &c.Team, &c.Color,
			&c.Lap, &c.PosX, &c.PosY, &c.SpeedKph,
			&c.RPM, &c.Gear, &c.Throttle, &c.Brake, &c.DRS,
			&c.BestLapMs, &c.LastLapMs,
		); err != nil {
			return stateSnapshot{}, err
		}
		cars = append(cars, c)
	}
	return stateSnapshot{
		Session: sess,
		Cars:    cars,
		StampMs: time.Now().UnixMilli(),
	}, rows.Err()
}

// handleLeaderboard is an HTMX-friendly HTML fragment returning a sorted
// leaderboard. The client polls this via hx-trigger="every 500ms".
func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var sessID int64
	if err := s.Pool.QueryRow(ctx, `
		SELECT session_id FROM sessions
		ORDER BY (ended_at IS NULL) DESC, started_at DESC LIMIT 1
	`).Scan(&sessID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// "Race position" here is a simple approximation: lap * 1.0 + track_pos.
	// Real F1 would use sector crossings, but this is good enough for a demo.
	rows, err := s.Pool.Query(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (driver_id)
			       driver_id, ts, lap, track_pos
			FROM telemetry
			WHERE session_id = $1
			ORDER BY driver_id, ts DESC
		),
		lap_stats AS (
			SELECT driver_id,
			       MIN(lap_time_ms) AS best_lap_ms,
			       MAX(lap_number)  AS max_lap_number
			FROM laps WHERE session_id = $1 GROUP BY driver_id
		),
		last_lap AS (
			SELECT lp.driver_id, lp.lap_time_ms AS last_lap_ms
			FROM laps lp
			JOIN lap_stats ls
			  ON ls.driver_id = lp.driver_id
			 AND ls.max_lap_number = lp.lap_number
			WHERE lp.session_id = $1
		)
		SELECT d.code, d.team, d.color_hex,
		       l.lap, l.track_pos,
		       COALESCE(s.best_lap_ms, 0), COALESCE(ll.last_lap_ms, 0),
		       (MAX(l.lap + l.track_pos) OVER () - (l.lap + l.track_pos)) AS gap_laps
		FROM latest l
		JOIN drivers d ON d.driver_id = l.driver_id
		LEFT JOIN lap_stats s ON s.driver_id = l.driver_id
		LEFT JOIN last_lap  ll ON ll.driver_id = l.driver_id
		ORDER BY (l.lap + l.track_pos) DESC
	`, sessID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type row struct {
		Pos   int
		Code  string
		Team  string
		Color string
		Lap   int
		Best  string
		Last  string
		Gap   string
	}
	var out []row
	leaderLastMs := 0
	i := 0
	for rows.Next() {
		i++
		var (
			code, team, color string
			lap               int
			tp, gapLaps       float64
			best, last        int
		)
		if err := rows.Scan(&code, &team, &color, &lap, &tp, &best, &last, &gapLaps); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if i == 1 {
			leaderLastMs = last
		}
		out = append(out, row{
			Pos:   i,
			Code:  code,
			Team:  team,
			Color: color,
			Lap:   lap,
			Best:  fmtLap(best),
			Last:  fmtLap(last),
			Gap:   fmtGap(i, gapLaps, leaderLastMs),
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "leaderboard.html", out); err != nil {
		log.Printf("leaderboard tpl: %v", err)
	}
}

// handleSpeedMap returns avg speed per 1%-of-lap bucket across the whole
// session — a 100-row aggregation over the hot `telemetry` table that the
// simulator is INSERTing into. The dashboard polls this every few seconds
// and renders the result as a colored ribbon along the racing line, so the
// audience can watch the analytical query refresh against the same table
// the inserts are hitting.
func (s *Server) handleSpeedMap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var sessID int64
	if err := s.Pool.QueryRow(ctx, `
		SELECT session_id FROM sessions
		ORDER BY (ended_at IS NULL) DESC, started_at DESC LIMIT 1
	`).Scan(&sessID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT WIDTH_BUCKET(track_pos, 0, 1, 100) AS bucket,
		       AVG(speed_kph)                    AS avg_kph,
		       COUNT(*)                          AS samples
		FROM telemetry
		WHERE session_id = $1
		GROUP BY bucket
		ORDER BY bucket
	`, sessID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type bucket struct {
		Bucket  int     `json:"bucket"`
		AvgKph  float64 `json:"avg_kph"`
		Samples int64   `json:"samples"`
	}
	var out []bucket
	for rows.Next() {
		var b bucket
		if err := rows.Scan(&b.Bucket, &b.AvgKph, &b.Samples); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, b)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"buckets": out})
}

// handleIngestRate returns the recent ingest rate and cumulative row count
// for the live session — a tiny query that's deliberately cheap to run, but
// whose entire point is to demonstrate the database measuring itself while
// it's also handling the simulator's INSERT stream. The dashboard polls this
// once per second and displays it in the footer.
func (s *Server) handleIngestRate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var sessID int64
	if err := s.Pool.QueryRow(ctx, `
		SELECT session_id FROM sessions
		ORDER BY (ended_at IS NULL) DESC, started_at DESC LIMIT 1
	`).Scan(&sessID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var rowsLast1s, totalRows int64
	err := s.Pool.QueryRow(ctx, `
		SELECT
		    COUNT(*) FILTER (WHERE ts > now() - interval '1 second') AS rows_last_1s,
		    COUNT(*)                                                  AS total_rows
		FROM telemetry
		WHERE session_id = $1
	`, sessID).Scan(&rowsLast1s, &totalRows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"rows_per_sec": rowsLast1s,
		"total_rows":   totalRows,
	})
}

func fmtLap(ms int) string {
	if ms <= 0 {
		return "—"
	}
	m := ms / 60000
	rem := ms - m*60000
	sec := rem / 1000
	frac := rem - sec*1000
	if m > 0 {
		return strconv.Itoa(m) + ":" + zPad(sec, 2) + "." + zPad(frac, 3)
	}
	return strconv.Itoa(sec) + "." + zPad(frac, 3)
}

// fmtGap formats a "gap to leader" cell for the leaderboard. Inputs:
//   - pos: 1-based finishing-order position (1 = leader).
//   - gapLaps: gap in lap-fractions (leader_pos - this_pos), so 0.05 = 5%
//     of a lap.
//   - leaderLastMs: the leader's most recent lap time in ms, used to convert
//     gapLaps into seconds. If 0 (leader hasn't completed a lap yet), the
//     gap is shown as a lap-fraction instead.
//
// Output:
//   - "—" for the leader
//   - "+N LAP[S]" when at least one full lap behind
//   - "+S.SSSs" using the leader's lap time when available
//   - "+0.XXX L" as a fallback when the leader hasn't set a lap yet
func fmtGap(pos int, gapLaps float64, leaderLastMs int) string {
	if pos == 1 {
		return "—"
	}
	if gapLaps >= 1.0 {
		n := int(gapLaps)
		if n == 1 {
			return "+1 LAP"
		}
		return "+" + strconv.Itoa(n) + " LAPS"
	}
	if leaderLastMs > 0 {
		gapSec := gapLaps * float64(leaderLastMs) / 1000.0
		// "+S.SSSs" — show three decimals for small gaps to read like F1 TV.
		whole := int(gapSec)
		frac := int((gapSec - float64(whole)) * 1000)
		return "+" + strconv.Itoa(whole) + "." + zPad(frac, 3) + "s"
	}
	// No leader lap time yet — fall back to fractional lap.
	whole := int(gapLaps)
	frac := int((gapLaps - float64(whole)) * 1000)
	return "+" + strconv.Itoa(whole) + "." + zPad(frac, 3) + " L"
}

func zPad(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}
