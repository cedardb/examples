// Package web is the operator-facing dashboard.
//
// Layout (top → bottom): active-alerts queue, live event stream, customer
// drill-down, ingest-rate footer. Each panel is driven by a different
// query against the same CedarDB instance the simulator is INSERTing into,
// at refresh rates from 200 ms to 2 s — that's the whole story this demo
// is selling.
package web

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// envDuration reads a Go duration string from the named environment
// variable, falling back to the supplied default. Mirrors the helper in
// the sim package so neither has to import the other.
func envDuration(name string, fallback time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("envDuration: invalid %s=%q (%v); using default %s", name, v, err, fallback)
		return fallback
	}
	return d
}

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

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
	mux.HandleFunc("/sse/events", s.handleSSE)
	mux.HandleFunc("/api/alerts", s.handleAlerts)
	mux.HandleFunc("/api/drilldown", s.handleDrilldown)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/storage", s.handleStorage)
	mux.Handle("/static/", http.StripPrefix("/static/", s.static))
	return mux
}

// --------------------------------------------------------------------- index

// indexData carries the dashboard's polling cadences into the index
// template. The four refresh intervals are millisecond integers so the
// JS setInterval() calls and htmx "every Nms" triggers can use them
// verbatim. Defaults match the original hardcoded values.
type indexData struct {
	AlertsRefreshMs    int64
	DrilldownRefreshMs int64
	StatsRefreshMs     int64
	StorageRefreshMs   int64
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := indexData{
		AlertsRefreshMs:    envDuration("HG_ALERTS_REFRESH", 1*time.Second).Milliseconds(),
		DrilldownRefreshMs: envDuration("HG_DRILLDOWN_REFRESH", 2*time.Second).Milliseconds(),
		StatsRefreshMs:     envDuration("HG_STATS_REFRESH", 1*time.Second).Milliseconds(),
		StorageRefreshMs:   envDuration("HG_STORAGE_REFRESH", 1*time.Second).Milliseconds(),
	}
	if err := s.tpl.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Printf("template: %v", err)
	}
}

// --------------------------------------------------------------------- alerts

// handleAlerts returns the operator's active-alerts queue as an HTML
// fragment. Joins alerts × households × plans so we can compute the
// per-alert SLA countdown (plan.sla_seconds - age) directly in the query.
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.Pool.Query(ctx, `
		SELECT a.alert_id, a.severity, a.detail,
		       EXTRACT(EPOCH FROM (now() - a.raised_at))::int AS age_s,
		       p.sla_seconds,
		       p.sla_seconds - EXTRACT(EPOCH FROM (now() - a.raised_at))::int AS sla_remaining,
		       h.household_id, h.address_hash,
		       p.name AS plan_name,
		       r.name AS region_name,
		       r.dispatch_center
		FROM alerts a
		JOIN households h ON h.household_id = a.household_id
		JOIN plans      p ON p.plan_id      = h.plan_id
		JOIN regions    r ON r.region_id    = h.region_id
		WHERE a.status = 'active'
		ORDER BY a.severity DESC, a.raised_at ASC
		LIMIT 25
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type row struct {
		AlertID      int64
		Severity     int
		Detail       string
		AgeSec       int
		SLASec       int
		SLARemaining int
		HouseholdID  int64
		AddressHash  string
		PlanName     string
		RegionName   string
		Dispatch     string
		Breached     bool
		AgeFmt       string
		SLAFmt       string
	}
	var out []row
	for rows.Next() {
		var rr row
		if err := rows.Scan(
			&rr.AlertID, &rr.Severity, &rr.Detail, &rr.AgeSec,
			&rr.SLASec, &rr.SLARemaining, &rr.HouseholdID, &rr.AddressHash,
			&rr.PlanName, &rr.RegionName, &rr.Dispatch,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rr.Breached = rr.SLARemaining < 0
		rr.AgeFmt = fmtDuration(rr.AgeSec)
		rr.SLAFmt = fmtDuration(absInt(rr.SLARemaining))
		out = append(out, rr)
	}
	// If the iteration ended because of an error (most commonly a
	// context-cancelled mid-scan on an unindexed query), surface it.
	// Without this, a half-completed scan looks identical to "no rows"
	// and the dashboard silently renders "no active alerts."
	if err := rows.Err(); err != nil {
		log.Printf("alerts query: rows.Err: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "alerts.html", out); err != nil {
		log.Printf("alerts tpl: %v", err)
	}
}

// --------------------------------------------------------------------- SSE

// handleSSE streams a JSON snapshot of the most-recent non-heartbeat
// events every 200 ms. Heartbeats are filtered out server-side so the
// operator panel stays readable; the analytical queries elsewhere
// continue to count every row.
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
	tick := time.NewTicker(envDuration("HG_SSE_INTERVAL", 200*time.Millisecond))
	defer tick.Stop()

	if err := s.pushEventStream(ctx, w, flusher); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.pushEventStream(ctx, w, flusher); err != nil {
				return
			}
		}
	}
}

type eventRow struct {
	EventID     int64   `json:"event_id"`
	Ts          string  `json:"ts"`
	HouseholdID int64   `json:"household_id"`
	AddressHash string  `json:"address_hash"`
	DeviceCode  string  `json:"device_code"`
	Location    string  `json:"location"`
	Kind        int     `json:"kind"`
	Severity    int     `json:"severity"`
	BatteryPct  int     `json:"battery_pct"`
	Region      string  `json:"region"`
}

func (s *Server) pushEventStream(ctx context.Context, w http.ResponseWriter, f http.Flusher) error {
	// Newest 25 non-heartbeat events, joined with device + device_type +
	// household + region for human-readable rendering. The (kind > 0)
	// filter uses the events_kind_ts_idx index efficiently.
	rows, err := s.Pool.Query(ctx, `
		SELECT e.event_id, e.ts, e.household_id, h.address_hash,
		       dt.code, d.location, e.kind, e.severity,
		       COALESCE(e.battery_pct, -1), r.name
		FROM events e
		JOIN devices       d  ON d.device_id     = e.device_id
		JOIN device_types  dt ON dt.device_type_id = d.device_type_id
		JOIN households    h  ON h.household_id  = e.household_id
		JOIN regions       r  ON r.region_id     = h.region_id
		WHERE e.kind > 0
		ORDER BY e.ts DESC
		LIMIT 25
	`)
	if err != nil {
		log.Printf("event stream query: %v", err)
		return nil
	}
	defer rows.Close()

	out := make([]eventRow, 0, 25)
	for rows.Next() {
		var er eventRow
		var ts time.Time
		var bp int
		if err := rows.Scan(&er.EventID, &ts, &er.HouseholdID, &er.AddressHash,
			&er.DeviceCode, &er.Location, &er.Kind, &er.Severity,
			&bp, &er.Region); err != nil {
			log.Printf("event stream scan: %v", err)
			return nil
		}
		er.Ts = ts.Format("15:04:05")
		er.BatteryPct = bp
		out = append(out, er)
	}
	if err := rows.Err(); err != nil {
		log.Printf("event stream: rows.Err: %v", err)
		// Don't propagate as a stream error — just skip this frame and
		// let the next tick try again.
		return nil
	}
	buf, err := json.Marshal(map[string]any{
		"events":   out,
		"stamp_ms": time.Now().UnixMilli(),
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", buf); err != nil {
		return err
	}
	f.Flush()
	return nil
}

// --------------------------------------------------------------------- drilldown

// handleDrilldown returns the "what's going on at this household right
// now" panel. We pick the household behind the highest-severity currently-
// active alert (or the most-recently-active household if none) and show
// its last 20 events with device + type joined in.
//
// This is the operator-flow query: alert fires → click household → see
// context to decide dispatch.
func (s *Server) handleDrilldown(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var householdID int64
	err := s.Pool.QueryRow(ctx, `
		SELECT a.household_id
		FROM alerts a
		WHERE a.status = 'active'
		ORDER BY a.severity DESC, a.raised_at ASC
		LIMIT 1
	`).Scan(&householdID)
	if err != nil {
		// Fallback: pick the household with the most events in the last
		// 5 minutes — keeps the panel populated even when there are no
		// active alerts.
		_ = s.Pool.QueryRow(ctx, `
			SELECT household_id
			FROM events
			WHERE ts > now() - interval '5 minutes'
			GROUP BY household_id
			ORDER BY COUNT(*) DESC
			LIMIT 1
		`).Scan(&householdID)
	}
	if householdID == 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<p class="muted">waiting for events…</p>`))
		return
	}

	// Household metadata for the header.
	var (
		addr, planName, regionName, dispatch string
		armed                                bool
		slaSec                               int
	)
	if err := s.Pool.QueryRow(ctx, `
		SELECT h.address_hash, h.armed, p.name, p.sla_seconds, r.name, r.dispatch_center
		FROM   households h
		JOIN   plans      p ON p.plan_id   = h.plan_id
		JOIN   regions    r ON r.region_id = h.region_id
		WHERE  h.household_id = $1
	`, householdID).Scan(&addr, &armed, &planName, &slaSec, &regionName, &dispatch); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Last 20 events for this household, any kind.
	rows, err := s.Pool.Query(ctx, `
		SELECT e.ts, dt.code, d.location, e.kind, e.severity,
		       COALESCE(e.battery_pct, -1)
		FROM   events e
		JOIN   devices      d  ON d.device_id      = e.device_id
		JOIN   device_types dt ON dt.device_type_id = d.device_type_id
		WHERE  e.household_id = $1
		ORDER BY e.ts DESC
		LIMIT 20
	`, householdID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type evRow struct {
		Ts         string
		Code       string
		Location   string
		Kind       int
		Severity   int
		BatteryPct int
	}
	var events []evRow
	for rows.Next() {
		var (
			er evRow
			ts time.Time
		)
		if err := rows.Scan(&ts, &er.Code, &er.Location, &er.Kind, &er.Severity, &er.BatteryPct); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		er.Ts = ts.Format("15:04:05")
		events = append(events, er)
	}
	if err := rows.Err(); err != nil {
		log.Printf("drilldown query: rows.Err: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "drilldown.html", map[string]any{
		"HouseholdID": householdID,
		"Address":     addr,
		"Armed":       armed,
		"Plan":        planName,
		"SLASec":      slaSec,
		"Region":      regionName,
		"Dispatch":    dispatch,
		"Events":      events,
	}); err != nil {
		log.Printf("drilldown tpl: %v", err)
	}
}

// --------------------------------------------------------------------- stats

// handleStats is the meta-query for the dashboard footer: how fast are
// we ingesting right now, how many rows have landed total, how many
// alerts are open. The dashboard footer reads this every
// HG_STATS_REFRESH (default 1s).
//
// The event-rate side reads from storage_samples (which the simulator
// populates every HG_STORAGE_SAMPLER_INTERVAL from its atomic eventID
// counter), not from events itself. At billion-row scale, scanning
// events for COUNT(*) — especially with no useful index — takes long
// enough to make the footer effectively frozen. storage_samples is a
// few hundred rows at most and answers instantly. The published
// rows_per_sec is therefore the average over the last sample interval
// (default 5s, the user may have set it longer in HG_STORAGE_SAMPLER_INTERVAL),
// not a strict one-second window.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var (
		rowsPerSec int64
		totalEv    int64
		activeAlts int64
		totalAlts  int64
	)
	// Event metrics — derived from the two most recent storage_samples
	// rows. total_events = latest_bytes / 48. rate = bytes-delta over the
	// elapsed time divided by 48.
	var (
		latestBytes, priorBytes sql.NullInt64
		latestTs, priorTs       sql.NullTime
	)
	if err := s.Pool.QueryRow(ctx, `
		SELECT
		    (SELECT uncompressed_bytes FROM storage_samples
		        ORDER BY sampled_at DESC LIMIT 1) AS latest_bytes,
		    (SELECT sampled_at         FROM storage_samples
		        ORDER BY sampled_at DESC LIMIT 1) AS latest_ts,
		    (SELECT uncompressed_bytes FROM storage_samples
		        WHERE sampled_at < (SELECT MAX(sampled_at) FROM storage_samples)
		        ORDER BY sampled_at DESC LIMIT 1) AS prior_bytes,
		    (SELECT sampled_at         FROM storage_samples
		        WHERE sampled_at < (SELECT MAX(sampled_at) FROM storage_samples)
		        ORDER BY sampled_at DESC LIMIT 1) AS prior_ts
	`).Scan(&latestBytes, &latestTs, &priorBytes, &priorTs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if latestBytes.Valid {
		totalEv = latestBytes.Int64 / int64(eventsRowBytes)
	}
	if latestBytes.Valid && priorBytes.Valid && latestTs.Valid && priorTs.Valid {
		dt := latestTs.Time.Sub(priorTs.Time).Seconds()
		if dt > 0 {
			delta := latestBytes.Int64 - priorBytes.Int64
			if delta < 0 {
				delta = 0
			}
			rowsPerSec = int64(float64(delta) / dt / float64(eventsRowBytes))
		}
	}
	// Alert metrics.
	if err := s.Pool.QueryRow(ctx, `
		SELECT
		    COUNT(*) FILTER (WHERE status = 'active') AS active,
		    COUNT(*)                                   AS total
		FROM alerts
	`).Scan(&activeAlts, &totalAlts); err != nil {
		// Non-fatal; alerts table may be empty very early.
		activeAlts = 0
		totalAlts = 0
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"rows_per_sec":  rowsPerSec,
		"total_events":  totalEv,
		"active_alerts": activeAlts,
		"total_alerts":  totalAlts,
	})
}

// --------------------------------------------------------------------- storage

// targetBytesPerSec: 3 TB/day uncompressed = 3 * 10^12 / 86 400 s ≈
// 34.722 MB/s. The dashboard renders gauges against this target.
const targetBytesPerSec = 34_722_222

// eventsRowBytes: per-row uncompressed footprint of the events table,
// derived from the schema column widths (matches the constant of the
// same name in the sim package). Used to convert storage_samples
// uncompressed_bytes back into row counts for the stats footer.
const eventsRowBytes = 48

// handleStorage returns the most recent uncompressed-size sample plus
// rates computed across the last 1, 5, and 15 minutes. Rates are
// (latest_bytes - bytes_at_window_start) / window_seconds — so they
// reflect the real growth of stored data, not the wire-rate of inserts.
func (s *Server) handleStorage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	type windowResult struct {
		LatestBytes sql.NullInt64
		LatestTs    sql.NullTime
		Bytes1mAgo  sql.NullInt64
		Ts1mAgo     sql.NullTime
		Bytes5mAgo  sql.NullInt64
		Ts5mAgo     sql.NullTime
		Bytes15mAgo sql.NullInt64
		Ts15mAgo    sql.NullTime
	}
	var rr windowResult
	if err := s.Pool.QueryRow(ctx, `
		SELECT
		    (SELECT uncompressed_bytes FROM storage_samples
		        ORDER BY sampled_at DESC LIMIT 1) AS latest_bytes,
		    (SELECT sampled_at         FROM storage_samples
		        ORDER BY sampled_at DESC LIMIT 1) AS latest_ts,
		    (SELECT uncompressed_bytes FROM storage_samples
		        WHERE sampled_at <= now() - interval '1 minute'
		        ORDER BY sampled_at DESC LIMIT 1) AS bytes_1m,
		    (SELECT sampled_at         FROM storage_samples
		        WHERE sampled_at <= now() - interval '1 minute'
		        ORDER BY sampled_at DESC LIMIT 1) AS ts_1m,
		    (SELECT uncompressed_bytes FROM storage_samples
		        WHERE sampled_at <= now() - interval '5 minutes'
		        ORDER BY sampled_at DESC LIMIT 1) AS bytes_5m,
		    (SELECT sampled_at         FROM storage_samples
		        WHERE sampled_at <= now() - interval '5 minutes'
		        ORDER BY sampled_at DESC LIMIT 1) AS ts_5m,
		    (SELECT uncompressed_bytes FROM storage_samples
		        WHERE sampled_at <= now() - interval '15 minutes'
		        ORDER BY sampled_at DESC LIMIT 1) AS bytes_15m,
		    (SELECT sampled_at         FROM storage_samples
		        WHERE sampled_at <= now() - interval '15 minutes'
		        ORDER BY sampled_at DESC LIMIT 1) AS ts_15m
	`).Scan(
		&rr.LatestBytes, &rr.LatestTs,
		&rr.Bytes1mAgo, &rr.Ts1mAgo,
		&rr.Bytes5mAgo, &rr.Ts5mAgo,
		&rr.Bytes15mAgo, &rr.Ts15mAgo,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rate := func(latest, prev sql.NullInt64, latestTs, prevTs sql.NullTime) (float64, bool) {
		if !latest.Valid || !prev.Valid || !latestTs.Valid || !prevTs.Valid {
			return 0, false
		}
		dt := latestTs.Time.Sub(prevTs.Time).Seconds()
		if dt <= 0 {
			return 0, false
		}
		db := float64(latest.Int64 - prev.Int64)
		if db < 0 {
			db = 0 // can happen across CedarDB compactions
		}
		return db / dt, true
	}

	r1, ok1 := rate(rr.LatestBytes, rr.Bytes1mAgo, rr.LatestTs, rr.Ts1mAgo)
	r5, ok5 := rate(rr.LatestBytes, rr.Bytes5mAgo, rr.LatestTs, rr.Ts5mAgo)
	r15, ok15 := rate(rr.LatestBytes, rr.Bytes15mAgo, rr.LatestTs, rr.Ts15mAgo)

	out := map[string]any{
		"target_bytes_per_sec": targetBytesPerSec,
	}
	if rr.LatestBytes.Valid {
		out["latest_bytes"] = rr.LatestBytes.Int64
	}
	if rr.LatestTs.Valid {
		out["latest_ts"] = rr.LatestTs.Time.Format(time.RFC3339)
	}
	if ok1 {
		out["rate_1m_bps"] = r1
	}
	if ok5 {
		out["rate_5m_bps"] = r5
	}
	if ok15 {
		out["rate_15m_bps"] = r15
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// --------------------------------------------------------------------- utils

func fmtDuration(sec int) string {
	if sec < 60 {
		return strconv.Itoa(sec) + "s"
	}
	return strconv.Itoa(sec/60) + "m" + strconv.Itoa(sec%60) + "s"
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
