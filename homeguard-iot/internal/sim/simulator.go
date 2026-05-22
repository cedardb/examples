// Package sim generates the IoT telemetry stream and writes it to CedarDB.
//
// On startup the simulator upserts the dimension data (plans, regions,
// device_types) and synthesizes a fleet of households with ~10 devices
// each. At runtime the work splits into two layers:
//
//  1. N row-producer goroutines ("writers"). Each owns a slice of the
//     device fleet, ticks at TickHz, and produces its share of the per-
//     tick budget: heartbeats rotated round-robin over its partition,
//     a handful of triggered events, and very occasional battery_low /
//     tamper / offline events. Producers do NOT talk to the DB on the
//     hot path; they push completed row batches onto eventCh.
//
//  2. One ingestor goroutine. Drains eventCh, coalesces batches up to
//     ingestBatch rows (or every flushInterval, whichever comes first),
//     and writes them via a single pgx.CopyFrom. Because there is only
//     one CopyFrom in flight at a time, CedarDB never trips the
//     "cannot start bulk operation until previous bulk operation has
//     become globally visible" (SQLSTATE 40P01) error that fires when
//     two writer goroutines try to COPY in parallel.
//
// All producers share an atomic event_id counter so they don't collide
// on the BIGINT primary key. (event_id is plain BIGINT rather than
// BIGSERIAL — CedarDB rejects the COPY frame when the column has a
// sequence default; client-side ids dodge the problem entirely.)
//
// Triggered events are run through the alert rules (rules.go) and matching
// alerts are inserted into the alerts table. Background goroutines resolve
// old active alerts and shuffle households' armed state so the dashboard
// stays dynamic.
package sim

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// envDuration reads a Go duration string (e.g. "5s", "200ms", "1m30s")
// from the named environment variable, falling back to the supplied
// default if the variable is unset or unparseable. Used to externalise
// polling intervals so they can be tuned from docker-compose without a
// rebuild.
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

// envInt reads a positive integer from the named environment variable,
// falling back to the default if unset, unparseable, or <= 0.
func envInt(name string, fallback int) int {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		log.Printf("envInt: invalid %s=%q; using default %d", name, v, fallback)
		return fallback
	}
	return n
}

// envBool reads a boolean (strconv.ParseBool grammar: 1/t/T/TRUE/true/True
// or 0/f/F/FALSE/false/False) from the named environment variable,
// falling back to the supplied default if unset or unparseable.
func envBool(name string, fallback bool) bool {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Printf("envBool: invalid %s=%q; using default %v", name, v, fallback)
		return fallback
	}
	return b
}

// Tuning constants for the fan-in COPY drainer.
const (
	// ingestBatch is the row count at which the ingestor flushes a COPY.
	// Larger = better amortization of CopyFrom overhead; smaller = lower
	// latency and tighter alert-to-event ordering.
	ingestBatch = 10000

	// flushInterval caps how long pending rows can sit before a partial
	// COPY is forced. Keeps the dashboard's live event stream fresh even
	// when -rate is well below ingestBatch/flushInterval.
	flushInterval = 50 * time.Millisecond

	// eventChCapacity holds completed row batches awaiting ingest. Sized
	// for ~2 batches per writer at the highest -writers we'd expect, so
	// producers rarely block on send.
	eventChCapacity = 64

	// Auto-tune knobs for the alert resolver. The per-tick LIMIT is
	//   max(env_floor, deltaFired * autoTuneHeadroom + backlog * backlogDecay)
	// then capped at maxAutoLimit.
	autoTuneHeadroom = 1.2   // drain 20% faster than current generation
	backlogDecay     = 0.01  // chip 1% off the standing backlog per tick
	maxAutoLimit     = 100000 // ceiling per tick so a runaway UPDATE can't bury CedarDB
)

// Device is one physical sensor placed in a household.
type Device struct {
	ID         int64
	Household  int64
	TypeID     int
	Code       string // device type code; cached so we don't re-join per tick
	Location   string
	BatteryPct int
}

// Household is one customer.
type Household struct {
	ID          int64
	PlanID      int
	RegionID    int
	AddressHash string
	Armed       bool
}

// Simulator state. Read-only on the hot path after New() returns: writers
// hold pointers in but don't mutate any field except via eventID's atomic
// counter and the per-device BatteryPct (writers partition the device
// slice so no two writers ever touch the same Device).
//
// One exception: Household.Armed is read by writers (when evaluating
// alert rules on triggered events) and written by shuffleArmedLoop in a
// separate goroutine. armedMu protects that access — writers take it as
// a reader, the shuffler as a writer.
//
// eventCh is the producer→ingestor handoff. Writers push completed row
// batches onto it; one ingestor goroutine drains it and runs CopyFrom.
type Simulator struct {
	Pool             *pgxpool.Pool
	Households       []Household
	Devices          []Device
	TickHz           int
	TargetRate       int          // events per second total target
	Writers          int          // number of writer goroutines
	eventID          atomic.Int64 // shared id generator; seeded in Run()
	armedMu          sync.RWMutex // guards Household.Armed reads/writes
	eventCh          chan [][]any // producer→ingestor batch queue
	lastCopyUnixNano atomic.Int64 // UnixNano of last successful CopyFrom (0 if none yet)
	copyFailures     atomic.Int64 // total CopyFrom errors since startup

	// Resolver instrumentation. fireAlert bumps the appropriate fired
	// counter; the resolver bumps the resolved counters with the UPDATE's
	// CommandTag.RowsAffected. (fired - resolved) ≈ backlog in CedarDB,
	// computed in-process so we don't need a COUNT(*) on the alerts table.
	alertsFiredLow     atomic.Int64
	alertsFiredHigh    atomic.Int64
	alertsResolvedLow  atomic.Int64
	alertsResolvedHigh atomic.Int64
}

// New builds the simulator state: upserts dimensions, synthesizes the
// household & device fleet, but does not begin the loop. Call Run to
// start ticking.
func New(ctx context.Context, pool *pgxpool.Pool, householdCount, devicesPerHousehold, tickHz, targetRate, writers int) (*Simulator, error) {
	if writers < 1 {
		writers = 1
	}
	if err := upsertDimensions(ctx, pool); err != nil {
		return nil, fmt.Errorf("upsert dimensions: %w", err)
	}

	rng := rand.New(rand.NewSource(0xC1D4A12)) // deterministic fleet, demo-friendly

	hh, devs := generateFleet(rng, householdCount, devicesPerHousehold)
	log.Printf("synthesized fleet: %d households · %d devices", len(hh), len(devs))

	if err := upsertHouseholds(ctx, pool, hh); err != nil {
		return nil, fmt.Errorf("upsert households: %w", err)
	}
	if err := upsertDevices(ctx, pool, devs); err != nil {
		return nil, fmt.Errorf("upsert devices: %w", err)
	}

	return &Simulator{
		Pool:       pool,
		Households: hh,
		Devices:    devs,
		TickHz:     tickHz,
		TargetRate: targetRate,
		Writers:    writers,
	}, nil
}

// Run seeds the event_id counter, launches the background loops (alert
// resolution + armed-state shuffle + storage sampler), spawns the single
// ingestor goroutine that owns CopyFrom, and fans out s.Writers
// producer goroutines over partitions of the device fleet. Producers
// build row batches and push them onto s.eventCh; the ingestor drains.
func (s *Simulator) Run(ctx context.Context) error {
	var maxID int64
	if err := s.Pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(event_id), 0) FROM events`).Scan(&maxID); err != nil {
		return fmt.Errorf("seed event_id: %w", err)
	}
	s.eventID.Store(maxID)
	log.Printf("event_id counter seeded at %d", maxID)

	s.eventCh = make(chan [][]any, eventChCapacity)

	go s.resolveAlertsLoop(ctx)
	go s.shuffleArmedLoop(ctx)
	go s.storageSamplerLoop(ctx)

	// Ingestor pool — N goroutines all drain eventCh and run CopyFrom in
	// parallel. Default N=1 is the historical safe behaviour (CedarDB
	// used to reject overlapping COPYs with SQLSTATE 40P01); newer
	// CedarDB versions accept concurrent COPYs, in which case N > 1 may
	// give a meaningful throughput bump. Tune with HG_INGESTORS.
	numIngestors := envInt("HG_INGESTORS", 1)
	var ingestorWG sync.WaitGroup
	for i := 0; i < numIngestors; i++ {
		ingestorWG.Add(1)
		go func(id int) {
			defer ingestorWG.Done()
			s.ingestorLoop(ctx, id)
		}(i)
	}

	// Heartbeat: one line per second describing pipeline health. Lets us
	// distinguish "producers stopped" (delta=0) from "ingestor stuck"
	// (queue high, lastCopy ago growing) from "CedarDB rejecting"
	// (failures climbing).
	go s.heartbeatLoop(ctx)

	// Per-writer per-tick budget. The total target rate divides across
	// writers; each writer then divides its share across ticks.
	hbPerTick := s.TargetRate / s.TickHz / s.Writers
	if hbPerTick < 1 {
		hbPerTick = 1
	}
	log.Printf("simulator running: writers=%d tickHz=%d target=%d ev/s hb/tick/writer=%d fleet=%d devices · ingestors=%d batch=%d flush=%s",
		s.Writers, s.TickHz, s.TargetRate, hbPerTick, len(s.Devices),
		numIngestors, ingestBatch, flushInterval)

	var wg sync.WaitGroup
	for i := 0; i < s.Writers; i++ {
		w := &writer{
			id:        i,
			sim:       s,
			devices:   devicePartition(s.Devices, i, s.Writers),
			rng:       rand.New(rand.NewSource(time.Now().UnixNano() ^ int64(i)*0xC0DE)),
			hbPerTick: hbPerTick,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.run(ctx)
		}()
	}
	wg.Wait()
	// Producers are done; let the ingestors drain anything still pending.
	// Closing the channel is broadcast to every receiver, so all N
	// ingestors observe (zero, false) and exit cleanly.
	close(s.eventCh)
	ingestorWG.Wait()
	return ctx.Err()
}

// devicePartition returns the i-th slice of d, split into n contiguous
// chunks. The last partition absorbs any remainder.
func devicePartition(d []Device, i, n int) []Device {
	chunk := len(d) / n
	lo := i * chunk
	hi := lo + chunk
	if i == n-1 {
		hi = len(d)
	}
	return d[lo:hi]
}

// writer is one ingest goroutine. Owns a slice of the device fleet,
// keeps its own deviceCursor and rng, allocates fresh event_ids from
// sim.eventID, and writes each tick via pgx.CopyFrom on its own pool
// connection. Writers never share any mutable state on the hot path
// except the atomic id counter.
type writer struct {
	id           int
	sim          *Simulator
	devices      []Device
	rng          *rand.Rand
	deviceCursor int
	hbPerTick    int
}

func (w *writer) run(ctx context.Context) {
	interval := time.Second / time.Duration(w.sim.TickHz)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			w.emitTick(ctx)
		}
	}
}

// emitTick generates one tick's worth of events from this writer's
// device partition and hands the batch off to the ingestor goroutine
// via the simulator's eventCh. No DB I/O happens here.
func (w *writer) emitTick(ctx context.Context) {
	if len(w.devices) == 0 {
		return
	}
	now := time.Now()
	rows := make([][]any, 0, w.hbPerTick+8)

	// 1) Heartbeats: rotate through this writer's partition.
	for i := 0; i < w.hbPerTick; i++ {
		d := &w.devices[w.deviceCursor%len(w.devices)]
		w.deviceCursor++
		// Battery slowly drains over the demo; clamp at 5%.
		if w.rng.Intn(10000) < 3 && d.BatteryPct > 5 {
			d.BatteryPct--
		}
		rssi := -50 - w.rng.Intn(40) // -50 to -90 dBm
		var val float64
		if d.Code == "TEMP" {
			val = 18.0 + w.rng.Float64()*10
		}
		rows = append(rows, w.row(d, now, 0 /*heartbeat*/, 0, val, rssi))
	}

	// 2) Triggered events: a handful per tick from random devices in
	//    this writer's partition.
	triggers := 3 + w.rng.Intn(5)
	for i := 0; i < triggers; i++ {
		d := &w.devices[w.rng.Intn(len(w.devices))]
		hh := &w.sim.Households[d.Household%int64(len(w.sim.Households))]
		rssi := -50 - w.rng.Intn(40)
		rows = append(rows, w.row(d, now, 1 /*triggered*/, int(severityFor(d.Code)), 1.0, rssi))
		w.sim.armedMu.RLock()
		armed := hh.Armed
		w.sim.armedMu.RUnlock()
		if dec := EvaluateAlert(d.Code, 1, armed); dec != nil {
			w.sim.fireAlert(ctx, d.Household, dec.Severity, dec.Detail)
		}
	}

	// 3) Occasional battery_low / tamper / offline.
	if w.rng.Intn(100) < 5 {
		d := &w.devices[w.rng.Intn(len(w.devices))]
		kind := []int{2, 4, 3}[w.rng.Intn(3)]
		sev := 1
		if kind == 4 {
			sev = 4
		}
		rows = append(rows, w.row(d, now, kind, sev, 0.0, -60-w.rng.Intn(30)))
		if kind == 4 {
			if dec := EvaluateAlert(d.Code, 4, false); dec != nil {
				w.sim.fireAlert(ctx, d.Household, dec.Severity, dec.Detail)
			}
		}
	}

	// 4) Hand the batch to the ingestor. The send blocks if the channel
	//    is full — which is the right backpressure: it means the
	//    ingestor (CedarDB) can't keep up, so we shouldn't generate more
	//    rows until it can. ctx.Done unblocks for clean shutdown.
	if len(rows) == 0 {
		return
	}
	select {
	case w.sim.eventCh <- rows:
	case <-ctx.Done():
	}
}

// row builds one events row with a freshly-allocated event_id. The
// shared atomic counter means writers never collide on the BIGINT PK.
func (w *writer) row(d *Device, ts time.Time, kind, sev int, val float64, rssi int) []any {
	return []any{
		w.sim.eventID.Add(1),
		d.ID, d.Household, ts,
		int16(kind), int16(sev), val,
		int16(d.BatteryPct), int16(rssi),
	}
}

// ingestorLoop is the single CopyFrom point. Coalesces batches off of
// eventCh up to ingestBatch rows (or every flushInterval, whichever
// comes first) and runs one pgx.CopyFrom per flush. Because there's
// only ever one CopyFrom in flight, CedarDB never trips the
// "previous bulk operation must become globally visible" (SQLSTATE
// 40P01) error that fires when multiple goroutines COPY concurrently.
//
// Shutdown contract: Run() closes eventCh after all producers have
// exited, the ingestor drains everything still in flight, runs one
// final flush, and returns.
func (s *Simulator) ingestorLoop(ctx context.Context, id int) {
	batchSize := envInt("HG_INGEST_BATCH", ingestBatch)
	if id == 0 {
		log.Printf("ingestor: batchSize=%d flushInterval=%s", batchSize, flushInterval)
	}

	pending := make([][]any, 0, batchSize*2)
	cols := []string{
		"event_id", "device_id", "household_id", "ts",
		"kind", "severity", "value", "battery_pct", "rssi_dbm",
	}
	flush := func() {
		if len(pending) == 0 {
			return
		}
		if _, err := s.Pool.CopyFrom(ctx,
			pgx.Identifier{"events"}, cols, pgx.CopyFromRows(pending),
		); err != nil {
			s.copyFailures.Add(1)
			log.Printf("ingestor %d: copy events: %v (rows=%d)", id, err, len(pending))
		} else {
			s.lastCopyUnixNano.Store(time.Now().UnixNano())
		}
		pending = pending[:0]
	}

	flushTicker := time.NewTicker(flushInterval)
	defer flushTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case batch, ok := <-s.eventCh:
			if !ok {
				flush()
				return
			}
			pending = append(pending, batch...)
			if len(pending) >= batchSize {
				flush()
			}
		case <-flushTicker.C:
			flush()
		}
	}
}

// heartbeatLoop emits a one-line status report once a second so we can
// tell from the logs whether the pipeline is healthy or stuck, and where
// the stall is if it isn't:
//
//	queue=N/CAP        — eventCh depth. Near CAP means ingestor (CedarDB) is the bottleneck.
//	delta=N rows/s     — row generation rate observed via the atomic counter.
//	lastCopy=Xs ago    — wall time since the last successful CopyFrom. Should be < 1s under load.
//	copyFails=N        — cumulative CopyFrom errors; non-zero means CedarDB is rejecting.
//
// 0 ev/s in the dashboard footer + queue=CAP + lastCopy growing → CedarDB stopped accepting writes.
// 0 ev/s + queue=0 → producers stopped (look for goroutine panics).
func (s *Simulator) heartbeatLoop(ctx context.Context) {
	var prev int64
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			cur := s.eventID.Load()
			delta := cur - prev
			prev = cur
			ago := time.Duration(-1)
			if last := s.lastCopyUnixNano.Load(); last > 0 {
				ago = time.Since(time.Unix(0, last)).Round(time.Millisecond)
			}
			log.Printf("heartbeat: queue=%d/%d eventID=%d delta=%d rows/s lastCopy=%v ago copyFails=%d",
				len(s.eventCh), eventChCapacity, cur, delta, ago, s.copyFailures.Load())
		}
	}
}

// fireAlert inserts a row into alerts. Status starts 'active'; the
// background resolveAlertsLoop will resolve it after a short delay.
// We also bump the in-process generation counter the resolver uses to
// auto-tune its drain rate — we don't wait for the DB INSERT to settle
// because the resolver's controller wants the request rate, not the
// committed rate (they differ only by transient pool / fsync latency).
func (s *Simulator) fireAlert(ctx context.Context, householdID int64, severity int, detail string) {
	if severity <= 2 {
		s.alertsFiredLow.Add(1)
	} else {
		s.alertsFiredHigh.Add(1)
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO alerts (household_id, triggered_event_id, raised_at, severity, status, detail)
		VALUES ($1, 0, now(), $2, 'active', $3)
	`, householdID, int16(severity), detail)
	if err != nil {
		log.Printf("insert alert: %v", err)
	}
}

// resolveAlertsLoop runs in the background; every HG_RESOLVE_INTERVAL it
// resolves the oldest active alerts so the operator queue doesn't grow
// without bound. The mix of severities resolved varies (high-severity
// stick around longer, low-severity get auto-cleared faster) to mimic
// real triage.
//
// When HG_RESOLVE_AUTOTUNE is on (default), the per-tick LIMITs are
// chosen at runtime as
//
//     limit = max(env_floor,
//                 ceil(deltaFired * autoTuneHeadroom + backlog * backlogDecay))
//
// where deltaFired is alerts inserted since the previous tick, and
// backlog ≈ alertsFired − alertsResolved tracked in two atomic counters
// per severity tier. Both inputs come from in-process state so we never
// have to COUNT(*) the alerts table to size the next tick.
//
// HG_RESOLVE_LOW_LIMIT / HG_RESOLVE_HIGH_LIMIT remain the floors —
// auto-tune can push higher but never below them. Set
// HG_RESOLVE_AUTOTUNE=false to pin the limits at the floors (useful when
// you want to demo what happens to AGE/SLA as a backlog grows).
func (s *Simulator) resolveAlertsLoop(ctx context.Context) {
	interval := envDuration("HG_RESOLVE_INTERVAL", 2*time.Second)
	lowFloor := envInt("HG_RESOLVE_LOW_LIMIT", 2000)
	highFloor := envInt("HG_RESOLVE_HIGH_LIMIT", 600)
	autoTune := envBool("HG_RESOLVE_AUTOTUNE", true)
	log.Printf("alert resolver: interval=%s lowFloor=%d highFloor=%d autoTune=%v",
		interval, lowFloor, highFloor, autoTune)

	var lastFiredLow, lastFiredHigh int64
	var lastLogAt time.Time
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			curFiredLow := s.alertsFiredLow.Load()
			curFiredHigh := s.alertsFiredHigh.Load()
			deltaLow := curFiredLow - lastFiredLow
			deltaHigh := curFiredHigh - lastFiredHigh
			lastFiredLow, lastFiredHigh = curFiredLow, curFiredHigh

			backlogLow := curFiredLow - s.alertsResolvedLow.Load()
			backlogHigh := curFiredHigh - s.alertsResolvedHigh.Load()
			if backlogLow < 0 {
				backlogLow = 0
			}
			if backlogHigh < 0 {
				backlogHigh = 0
			}

			lowLimit, highLimit := lowFloor, highFloor
			if autoTune {
				autoLow := int(float64(deltaLow)*autoTuneHeadroom + float64(backlogLow)*backlogDecay)
				autoHigh := int(float64(deltaHigh)*autoTuneHeadroom + float64(backlogHigh)*backlogDecay)
				if autoLow > lowLimit {
					lowLimit = autoLow
				}
				if autoHigh > highLimit {
					highLimit = autoHigh
				}
			}
			if lowLimit > maxAutoLimit {
				lowLimit = maxAutoLimit
			}
			if highLimit > maxAutoLimit {
				highLimit = maxAutoLimit
			}

			// Resolve low-severity (info / sev 1-2) → false_alarm.
			lowTag, _ := s.Pool.Exec(ctx, `
				UPDATE alerts
				SET status = 'false_alarm',
				    resolved_at = now(),
				    resolution_ms = (EXTRACT(EPOCH FROM (now() - raised_at)) * 1000)::int
				WHERE alert_id IN (
					SELECT alert_id FROM alerts
					WHERE status = 'active' AND severity <= 2
					ORDER BY raised_at ASC LIMIT $1
				)
			`, lowLimit)
			// Dispatch then resolve a sample of higher-severity older alerts.
			highTag, _ := s.Pool.Exec(ctx, `
				UPDATE alerts
				SET status = 'resolved',
				    resolved_at = now(),
				    resolution_ms = (EXTRACT(EPOCH FROM (now() - raised_at)) * 1000)::int
				WHERE alert_id IN (
					SELECT alert_id FROM alerts
					WHERE status = 'active' AND severity >= 3
					  AND raised_at < now() - interval '20 seconds'
					ORDER BY raised_at ASC LIMIT $1
				)
			`, highLimit)
			s.alertsResolvedLow.Add(lowTag.RowsAffected())
			s.alertsResolvedHigh.Add(highTag.RowsAffected())

			// Once every 10 s, log what we drained and how big the queue is.
			if time.Since(lastLogAt) >= 10*time.Second {
				log.Printf("resolver: drain low=%d/%d high=%d/%d · backlog low=%d high=%d",
					lowTag.RowsAffected(), lowLimit,
					highTag.RowsAffected(), highLimit,
					backlogLow, backlogHigh)
				lastLogAt = time.Now()
			}
		}
	}
}

// shuffleArmedLoop runs as its own goroutine (replaces the
// armedTimer-driven path in the old emitTick). Every 10 seconds it
// flips ~5% of households' armed state so the rules engine outputs
// stay varied over the demo.
func (s *Simulator) shuffleArmedLoop(ctx context.Context) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano() ^ 0xA1ED))
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.shuffleArmedState(ctx, rng)
		}
	}
}

func (s *Simulator) shuffleArmedState(ctx context.Context, rng *rand.Rand) {
	// Flip ~5% of households' armed state in memory; push the new value
	// in batches. armedMu guards the in-memory mutation against
	// concurrent reads from the writer goroutines.
	batch := &pgx.Batch{}
	flipped := 0
	s.armedMu.Lock()
	for i := range s.Households {
		if rng.Intn(20) == 0 {
			s.Households[i].Armed = !s.Households[i].Armed
			batch.Queue(`UPDATE households SET armed = $1 WHERE household_id = $2`,
				s.Households[i].Armed, s.Households[i].ID)
			flipped++
		}
	}
	s.armedMu.Unlock()
	if flipped == 0 {
		return
	}
	if err := s.Pool.SendBatch(ctx, batch).Close(); err != nil {
		log.Printf("armed-state shuffle: %v", err)
	}
}

// eventsRowBytes is the per-row uncompressed footprint of the events
// table, derived directly from the schema column widths:
//
//	event_id BIGINT(8) + device_id BIGINT(8) + household_id BIGINT(8)
//	+ ts TIMESTAMPTZ(8) + kind SMALLINT(2) + severity SMALLINT(2)
//	+ value DOUBLE(8) + battery_pct SMALLINT(2) + rssi_dbm SMALLINT(2)
//	= 48 bytes
//
// We use this rather than CedarDB's cedardb_compression_info view
// because the view only updates once written data lands in column-store
// blocks — at high ingest rates that lags reality by many seconds.
// COUNT(*) on events tracks live row counts in real time.
const eventsRowBytes = 48

// storageSamplerLoop records a (now(), 48 * eventID) sample into
// storage_samples every 5 seconds. The dashboard's /api/storage endpoint
// derives 1m/5m/15m ingest rates from this table.
//
// We compute bytes from the in-process atomic counter (s.eventID, which
// is seeded from MAX(event_id) at startup and incremented per generated
// row) rather than running COUNT(*) on the events table. Two reasons:
//
//  1. Cost. At demo scale the events table can reach billions of rows
//     in well under an hour. COUNT(*) becomes a multi-second full scan
//     and may hold a snapshot that pressures the write path — a likely
//     cause of the 30-minute ingest stall we saw.
//  2. Currency. The atomic counter is updated at the moment a row is
//     generated, ahead of the CopyFrom that actually persists it. The
//     gauge can therefore appear up to (channel queue + in-flight
//     batch) rows ahead of what's truly in the table — at most ~160 K
//     rows ≈ 8 MB at the demo's default backpressure ceiling, which is
//     well under one second of headway and a fine trade-off given the
//     scan cost we avoid.
//
// The number is "uncompressed-bytes-equivalent" — what the data would
// weigh as a flat file. CedarDB's actual on-disk footprint after column
// encoding (truncate, frame-of-reference, dictionary, etc.) is 5–10×
// smaller.
func (s *Simulator) storageSamplerLoop(ctx context.Context) {
	interval := envDuration("HG_STORAGE_SAMPLER_INTERVAL", 5*time.Second)
	log.Printf("storage sampler: interval=%s", interval)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			bytes := int64(eventsRowBytes) * s.eventID.Load()
			if _, err := s.Pool.Exec(ctx, `
				INSERT INTO storage_samples (sampled_at, uncompressed_bytes)
				VALUES (now(), $1)
			`, bytes); err != nil {
				log.Printf("storage sampler: insert: %v", err)
			}
		}
	}
}

// severityFor maps a device type code to its baseline severity for
// triggered events. Used when writing the event row itself; the alert
// decision (rules.go) does the more nuanced household-armed logic.
func severityFor(code string) int16 {
	for _, dt := range DeviceTypes {
		if dt.Code == code {
			return int16(dt.DefaultSeverity)
		}
	}
	return 1
}

// --- fleet synthesis ------------------------------------------------------

func generateFleet(rng *rand.Rand, householdCount, devicesPerHousehold int) ([]Household, []Device) {
	hh := make([]Household, 0, householdCount)
	devs := make([]Device, 0, householdCount*devicesPerHousehold)

	for i := 0; i < householdCount; i++ {
		id := int64(1000000 + i) // start at a friendly-looking ID
		plan := pickWeighted(rng, []int{40, 35, 18, 7})         // basic-heavy
		region := rng.Intn(len(Regions)) + 1
		armed := rng.Intn(100) < 35 // ~35% armed at any time
		hh = append(hh, Household{
			ID:          id,
			PlanID:      plan + 1,
			RegionID:    region,
			AddressHash: hashAddr(id),
			Armed:       armed,
		})

		// Each household gets between (devicesPerHousehold/2) and
		// (devicesPerHousehold*3/2) devices, distributed across plausible
		// types and locations.
		n := devicesPerHousehold/2 + rng.Intn(devicesPerHousehold)
		for j := 0; j < n; j++ {
			dt := pickDeviceType(rng)
			loc := pickLocation(rng, dt.Code)
			devs = append(devs, Device{
				ID:         int64(len(devs)) + 100000,
				Household:  id,
				TypeID:     dt.ID,
				Code:       dt.Code,
				Location:   loc,
				BatteryPct: 70 + rng.Intn(30), // 70..99
			})
		}
	}
	return hh, devs
}

func pickWeighted(rng *rand.Rand, weights []int) int {
	total := 0
	for _, w := range weights {
		total += w
	}
	r := rng.Intn(total)
	for i, w := range weights {
		if r < w {
			return i
		}
		r -= w
	}
	return len(weights) - 1
}

func pickDeviceType(rng *rand.Rand) DeviceType {
	// Reasonable distribution: motion + door/window common; smoke/CO/water
	// less common but always at least one. Doorbell + keypad moderate.
	weights := []int{30, 22, 15, 5, 6, 4, 6, 4, 5, 3}
	idx := pickWeighted(rng, weights)
	return DeviceTypes[idx]
}

func pickLocation(rng *rand.Rand, code string) string {
	// Map device types to plausible locations.
	switch code {
	case "DOOR":
		return []string{"front_door", "back_door", "garage_door", "side_door"}[rng.Intn(4)]
	case "WINDOW":
		return []string{"living_room", "kitchen", "dining_room", "master_bedroom", "bedroom_2", "bedroom_3"}[rng.Intn(6)]
	case "GLASS_BREAK":
		return []string{"living_room", "dining_room", "kitchen"}[rng.Intn(3)]
	case "SMOKE", "CO":
		return []string{"kitchen", "hallway", "master_bedroom", "bedroom_2", "basement"}[rng.Intn(5)]
	case "WATER":
		return []string{"kitchen", "basement", "utility_room"}[rng.Intn(3)]
	case "TEMP":
		return []string{"living_room", "master_bedroom", "basement", "attic"}[rng.Intn(4)]
	case "DOORBELL":
		return "front_door"
	case "KEYPAD":
		return "front_door"
	case "MOTION":
		return Locations[rng.Intn(len(Locations))]
	}
	return Locations[rng.Intn(len(Locations))]
}

func hashAddr(id int64) string {
	h := sha1.Sum([]byte("addr-" + strconv.FormatInt(id, 10)))
	return hex.EncodeToString(h[:6]) // short, opaque
}

// --- dimension + fleet upserts -------------------------------------------

func upsertDimensions(ctx context.Context, pool *pgxpool.Pool) error {
	batch := &pgx.Batch{}
	for _, p := range Plans {
		batch.Queue(`
			INSERT INTO plans (plan_id, name, monthly_price_usd, sla_seconds)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (plan_id) DO UPDATE SET
				name = EXCLUDED.name,
				monthly_price_usd = EXCLUDED.monthly_price_usd,
				sla_seconds = EXCLUDED.sla_seconds
		`, p.ID, p.Name, p.PriceUSD, p.SLASeconds)
	}
	for _, r := range Regions {
		batch.Queue(`
			INSERT INTO regions (region_id, name, dispatch_center, timezone)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (region_id) DO UPDATE SET
				name = EXCLUDED.name,
				dispatch_center = EXCLUDED.dispatch_center,
				timezone = EXCLUDED.timezone
		`, r.ID, r.Name, r.DispatchCenter, r.Timezone)
	}
	for _, dt := range DeviceTypes {
		batch.Queue(`
			INSERT INTO device_types (device_type_id, code, name, default_severity)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (device_type_id) DO UPDATE SET
				code = EXCLUDED.code,
				name = EXCLUDED.name,
				default_severity = EXCLUDED.default_severity
		`, dt.ID, dt.Code, dt.Name, int16(dt.DefaultSeverity))
	}
	return pool.SendBatch(ctx, batch).Close()
}

func upsertHouseholds(ctx context.Context, pool *pgxpool.Pool, hh []Household) error {
	// CopyFrom into a temp staging approach would be faster, but a Batch of
	// INSERTs with ON CONFLICT is simple, idempotent, and fast enough for
	// ~30 000 rows once at startup.
	const batchSize = 1000
	for start := 0; start < len(hh); start += batchSize {
		end := start + batchSize
		if end > len(hh) {
			end = len(hh)
		}
		batch := &pgx.Batch{}
		for _, h := range hh[start:end] {
			batch.Queue(`
				INSERT INTO households (household_id, plan_id, region_id, address_hash, armed)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (household_id) DO UPDATE SET
					plan_id = EXCLUDED.plan_id,
					region_id = EXCLUDED.region_id,
					armed = EXCLUDED.armed
			`, h.ID, h.PlanID, h.RegionID, h.AddressHash, h.Armed)
		}
		if err := pool.SendBatch(ctx, batch).Close(); err != nil {
			return err
		}
	}
	return nil
}

func upsertDevices(ctx context.Context, pool *pgxpool.Pool, devs []Device) error {
	const batchSize = 1000
	for start := 0; start < len(devs); start += batchSize {
		end := start + batchSize
		if end > len(devs) {
			end = len(devs)
		}
		batch := &pgx.Batch{}
		for _, d := range devs[start:end] {
			batch.Queue(`
				INSERT INTO devices (device_id, household_id, device_type_id, location, last_battery_pct)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (device_id) DO UPDATE SET
					household_id = EXCLUDED.household_id,
					device_type_id = EXCLUDED.device_type_id,
					location = EXCLUDED.location,
					last_battery_pct = EXCLUDED.last_battery_pct
			`, d.ID, d.Household, d.TypeID, d.Location, int16(d.BatteryPct))
		}
		if err := pool.SendBatch(ctx, batch).Close(); err != nil {
			return err
		}
	}
	return nil
}
