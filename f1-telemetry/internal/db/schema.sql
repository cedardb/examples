-- F1 telemetry schema for CedarDB (Postgres dialect).
--
-- Five tables:
--   sessions   - one row per race / practice session
--   drivers    - 20 driver entries (one-time seed data)
--   telemetry  - high-frequency car state (this is the hot table: ~200 rows/sec)
--   laps       - one row per completed lap, with sector splits
--   events     - race events (start, fastest lap, overtake, etc.)
--
-- The dashboard joins telemetry x drivers for live state, and reads laps for
-- timing. All of this happens while the simulator is INSERT-ing into
-- telemetry/laps in real time, which is the CedarDB differentiator we want
-- to show: concurrent ingest + analytic queries on the same table.
--
-- This file is intentionally idempotent: every statement is guarded with
-- IF NOT EXISTS so the simulator can call it on every cold start without
-- risk of wiping prior data. For destructive re-init, run the simulator
-- with the `-reset-schema` flag, which DROPs each table first.

CREATE TABLE IF NOT EXISTS sessions (
    session_id   BIGSERIAL    PRIMARY KEY,
    name         TEXT         NOT NULL,
    track        TEXT         NOT NULL,
    total_laps   INTEGER      NOT NULL,
    started_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ended_at     TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS drivers (
    driver_id    INTEGER      PRIMARY KEY,
    code         TEXT         NOT NULL,           -- 3-letter code, e.g. 'VER'
    full_name    TEXT         NOT NULL,
    team         TEXT         NOT NULL,
    car_number   INTEGER      NOT NULL,
    color_hex    TEXT         NOT NULL            -- team color for the UI
);

-- Hot path. Inserted ~200 rows/sec (20 cars x 10 Hz).
CREATE TABLE IF NOT EXISTS telemetry (
    session_id   BIGINT            NOT NULL,
    driver_id    INTEGER           NOT NULL,
    ts           TIMESTAMPTZ       NOT NULL,
    lap          INTEGER           NOT NULL,
    track_pos    DOUBLE PRECISION  NOT NULL,   -- 0..1 around the lap
    pos_x        DOUBLE PRECISION  NOT NULL,
    pos_y        DOUBLE PRECISION  NOT NULL,
    speed_kph    DOUBLE PRECISION  NOT NULL,
    rpm          INTEGER           NOT NULL,
    gear         SMALLINT          NOT NULL,
    throttle     REAL              NOT NULL,   -- 0..1
    brake        REAL              NOT NULL,   -- 0..1
    drs          BOOLEAN           NOT NULL
);

-- Two indexes that match the two read patterns the dashboard uses:
--   1) latest sample per (session, driver)  -> track map + telemetry panel
--   2) latest samples across whole session  -> any "newest-first" scan
CREATE INDEX IF NOT EXISTS telemetry_session_driver_ts_idx
    ON telemetry (session_id, driver_id, ts DESC);

CREATE INDEX IF NOT EXISTS telemetry_session_ts_idx
    ON telemetry (session_id, ts DESC);

CREATE TABLE IF NOT EXISTS laps (
    session_id   BIGINT       NOT NULL,
    driver_id    INTEGER      NOT NULL,
    lap_number   INTEGER      NOT NULL,
    lap_time_ms  INTEGER      NOT NULL,
    sector1_ms   INTEGER      NOT NULL,
    sector2_ms   INTEGER      NOT NULL,
    sector3_ms   INTEGER      NOT NULL,
    finished_at  TIMESTAMPTZ  NOT NULL,
    PRIMARY KEY (session_id, driver_id, lap_number)
);

CREATE INDEX IF NOT EXISTS laps_session_lap_idx ON laps (session_id, lap_number);

CREATE TABLE IF NOT EXISTS events (
    event_id     BIGSERIAL    PRIMARY KEY,
    session_id   BIGINT       NOT NULL,
    driver_id    INTEGER,
    ts           TIMESTAMPTZ  NOT NULL,
    kind         TEXT         NOT NULL,         -- 'race_start','fastest_lap','overtake','drs_open'
    detail       TEXT
);

CREATE INDEX IF NOT EXISTS events_session_ts_idx ON events (session_id, ts DESC);
