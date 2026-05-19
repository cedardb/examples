# F1 Telemetry — CedarDB Live Demo

![F1 telemetry UI](./F1_Telemetry_UI_view.jpg)

A small Go project that simulates a 20 car Formula 1 race, streams 10 Hz
telemetry into CedarDB, and renders a live dashboard fed by the same database
the simulator writes into (e.g. an HTAP workload).

The point of the demo is the **concurrent ingest + analytic reads** story:
the simulator is `INSERT`-ing into `telemetry` and `laps` continuously while
the web server runs `SELECT`s against those tables every 200 ms to drive
the SSE stream.

What is _SSE_? SSE means _Server-Sent Events_: a streaming HTTP protocol where the server
keeps a long-lived response open and writes data: `{...}\n\n` chunks down it as
things happen, instead of the client polling repeatedly.

## Quick Start: Run it in Docker

```
docker compose up --build
```

* Open a browser tab to [the app](http://localhost:8080)
* Be sure to also click the _SQL queries_ [link](http://localhost:8080/static/queries.html) at the bottom of the page

## What's inside

```
schema.sql                  -- 5 tables: sessions, drivers, telemetry, laps, events
docker-compose.yml          -- CedarDB + simulator + web server
Dockerfile                  -- builds both Go binaries
cmd/simulator/main.go       -- runs the race
cmd/server/main.go          -- serves the dashboard
internal/db/db.go           -- pgx pool wrapper
internal/sim/track.go       -- Cedar Park Circuit: Catmull-Rom spline through
                               hand-picked control points (straight, hairpin,
                               chicane, sweepers)
internal/sim/simulator.go   -- 20 cars, 10 Hz, COPY-batched INSERTs
                               (per-driver pace + corner bias + form drift)
internal/web/server.go      -- HTTP + SSE + leaderboard HTML fragment + /static
internal/web/templates/     -- HTMX + small inline JS dashboard
internal/web/static/        -- embedded /static/ assets (logo)
```


## Run it (local Go, your own CedarDB)

If you already have CedarDB running:

```
export DATABASE_URL="postgres://USER:PASS@HOST:5432/DB?sslmode=disable"
psql "$DATABASE_URL" -f schema.sql

go run ./cmd/simulator -laps=50 -hz=10 -track=cedar-park &
go run ./cmd/server -addr=:8080
# open http://localhost:8080
```

## Picking a track

Six selectable circuits are baked in. Each is drawn from a hand-picked set
of control points run through a centripetal Catmull-Rom spline, with the
matching lap length chosen to put average speed in a realistic F1 band.

```
go run ./cmd/simulator -list-tracks
```

| key            | display name        | lap   | character                                                |
|----------------|---------------------|-------|----------------------------------------------------------|
| `cedar-park`   | Cedar Park Circuit  | 5.4 km| Long straight, fast right-hander, chicane, hairpin       |
| `sprint-oval`  | Sprint Oval         | 4.2 km| Two long straights, two big sweepers — fastest avg speed |
| `highline`     | Highline Esses      | 5.2 km| Flowing shallow esses across a long horizontal band      |
| `crescent`     | Crescent Bay        | 5.1 km| Asymmetric arc with a tight infield twist on the south   |
| `pinewood`     | Pinewood Climb      | 4.8 km| Vertical layout, two opposed hairpins, narrow infield    |
| `downtown`     | Downtown Loop       | 4.4 km| Street circuit — boxy, right-angle turns, slow avg       |

Use `-track <key>` to pick one, or `-track random` to let the simulator
choose. The dashboard reads the chosen preset's name from `sessions.track`
and rebuilds the same geometry — no track polyline is persisted, both
sides derive it from the same `internal/sim/track.go` preset map.

The dashboard also draws a checkered band across the start/finish line so
you can see where lap timing begins. Position and orientation come from
the `/track.json` endpoint, which returns the start point plus a unit
normal vector at trackPos = 0.

## Architecture

```
 ┌──────────────┐      INSERT (COPY)     ┌────────────────────┐
 │  simulator   │ ─────────────────────► │      CedarDB       │
 │  (20 cars,   │   telemetry + laps     │  sessions/drivers/ │
 │   10 Hz)     │                        │  telemetry/laps/   │
 └──────────────┘                        │  events            │
                                         └─────────┬──────────┘
                                                   │  4 concurrent reads:
                                                   │   - SSE snapshot   (every 200 ms)
                                                   │   - leaderboard    (every 500 ms)
                                                   │   - speed-map agg  (every 3 s)
                                                   │   - ingest-rate    (every 1 s)
                                                   ▼
                                         ┌────────────────────┐
                                         │   Go web server    │
                                         │   /sse/state       │
                                         │   /api/leaderboard │
                                         │   /api/speed-map   │
                                         │   /api/ingest-rate │
                                         └─────────┬──────────┘
                                                   ▼
                                         ┌────────────────────┐
                                         │   browser (HTMX)   │
                                         │   track map +      │
                                         │   speed ribbon +   │
                                         │   leaderboard +    │
                                         │   live ingest rate │
                                         └────────────────────┘
```

All four reads run against the same `telemetry` table the simulator is
INSERTing into. The speed-map and ingest-rate panels exist specifically to
make the concurrent-OLAP-on-OLTP story visible without saying it out loud.

## Key queries to read

The single query worth opening the source for is the SSE snapshot in
`internal/web/server.go::loadSnapshot`:

```sql
WITH latest AS (
  SELECT DISTINCT ON (driver_id)
         driver_id, lap, pos_x, pos_y, speed_kph,
         rpm, gear, throttle, brake, drs
  FROM telemetry
  WHERE session_id = $1
  ORDER BY driver_id, ts DESC
),
lap_stats AS (
  SELECT driver_id,
         MIN(lap_time_ms)                                      AS best_lap_ms,
         (ARRAY_AGG(lap_time_ms ORDER BY lap_number DESC))[1]  AS last_lap_ms
  FROM laps WHERE session_id = $1 GROUP BY driver_id
)
SELECT ... FROM latest JOIN drivers JOIN lap_stats ...
```

This runs five times a second over the same table the simulator is
inserting into. `DISTINCT ON` aligns with the
`(session_id, driver_id, ts DESC)` index so the latest row per driver is
returned without scanning the whole partition.

## Knobs

```
go run ./cmd/simulator -laps=50 -hz=10
                              ↑     ↑
                              │     └─ inserts/sec = 20 * hz (200 default)
                              └─ how many laps before the session ends
```

Try `-hz=50` for ~1000 inserts/sec, or run multiple simulators against the
same DB (each creates its own `session_id`).

