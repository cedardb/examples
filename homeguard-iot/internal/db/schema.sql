-- HomeGuard IoT demo schema for CedarDB (Postgres dialect).
--
-- The story: a home-security operator running ~3-4 TB/day of incoming IoT
-- events from millions of devices. Currently they pipe everything into
-- BigQuery for offline analytics and run a parallel real-time stack
-- (Pub/Sub + Dataflow + a KV store) for the monitoring center.
--
-- The CedarDB pitch is "collapse those two stacks: same table, OLTP-style
-- writes and OLAP-style reads, no replication lag, real joins across the
-- normalized dimension model."
--
-- Tables:
--   plans          - subscription tiers; SLA in seconds per tier
--   regions        - service regions / dispatch centers
--   device_types   - sensor catalog (motion, door, smoke, etc.)
--   households    -- customers; FK to plan + region
--   devices       -- sensors; FK to household + device_type
--   events         - HOT TABLE; up to ~500K rows/sec in the high-rate demo
--   alerts         - rules-derived alerts; smaller volume; the operator queue
--
-- This file is idempotent (CREATE TABLE IF NOT EXISTS on everything) so the
-- simulator can safely apply it on every cold start. For destructive reset
-- pass -reset-schema to the simulator.

CREATE TABLE IF NOT EXISTS plans (
    plan_id           INTEGER PRIMARY KEY,
    name              TEXT NOT NULL,
    monthly_price_usd NUMERIC(8, 2) NOT NULL,
    sla_seconds       INTEGER NOT NULL  -- monitoring response SLA
);

CREATE TABLE IF NOT EXISTS regions (
    region_id        INTEGER PRIMARY KEY,
    name             TEXT NOT NULL,
    dispatch_center  TEXT NOT NULL,
    timezone         TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS device_types (
    device_type_id   INTEGER PRIMARY KEY,
    code             TEXT NOT NULL,  -- MOTION, DOOR, WINDOW, GLASS_BREAK, SMOKE, CO, WATER, TEMP, DOORBELL, KEYPAD
    name             TEXT NOT NULL,
    default_severity SMALLINT NOT NULL
);

CREATE TABLE IF NOT EXISTS households (
    household_id  BIGINT PRIMARY KEY,
    plan_id       INTEGER NOT NULL,
    region_id     INTEGER NOT NULL,
    address_hash  TEXT NOT NULL,                  -- hashed for privacy
    enrolled_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    armed         BOOLEAN NOT NULL DEFAULT false  -- current armed state
);

CREATE TABLE IF NOT EXISTS devices (
    device_id        BIGINT PRIMARY KEY,
    household_id     BIGINT NOT NULL,
    device_type_id   INTEGER NOT NULL,
    location         TEXT NOT NULL,    -- 'front_door', 'kitchen', 'master_bedroom', ...
    installed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_battery_pct SMALLINT
);

-- Hot table. ~500K rows/sec in the high-rate demo. Billions of rows over
-- time. event_id is plain BIGINT (not BIGSERIAL) — the simulator generates
-- ids from a client-side atomic counter so it can use the binary COPY path,
-- which CedarDB rejects for BIGSERIAL defaults ("unable to cast from void
-- to bigint"). Multiple writer goroutines share the same id space via the
-- atomic counter.
CREATE TABLE IF NOT EXISTS events (
    event_id      BIGINT NOT NULL,
    device_id     BIGINT NOT NULL,
    household_id  BIGINT NOT NULL,   -- denormalized to skip a join on the hot path
    ts            TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind          SMALLINT NOT NULL, -- 0=heartbeat, 1=triggered, 2=battery_low, 3=offline, 4=tamper
    severity      SMALLINT NOT NULL, -- 0=normal, 1..5 escalating
    value         DOUBLE PRECISION,  -- sensor reading (temp °C, motion confidence, etc.)
    battery_pct   SMALLINT,
    rssi_dbm      SMALLINT           -- wireless signal strength
);

CREATE TABLE IF NOT EXISTS alerts (
    alert_id           BIGSERIAL PRIMARY KEY,
    household_id       BIGINT NOT NULL,
    triggered_event_id BIGINT NOT NULL,
    raised_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    severity           SMALLINT NOT NULL,
    status             TEXT NOT NULL,        -- 'active', 'dispatched', 'resolved', 'false_alarm'
    detail             TEXT,
    resolved_at        TIMESTAMPTZ,
    resolution_ms      INTEGER
);

-- Periodic storage-growth samples: one row per ~5s, sourced from
-- CedarDB's cedardb_compression_info system view. The dashboard's
-- /api/storage endpoint derives 1m/5m/15m ingest rates from this table.
-- sampled_at uses now()'s microsecond precision so it is unique at the
-- sampler's 5-second cadence.
CREATE TABLE IF NOT EXISTS storage_samples (
    sampled_at         TIMESTAMPTZ PRIMARY KEY,
    uncompressed_bytes BIGINT NOT NULL
);

CREATE INDEX alerts_status_raised_idx ON alerts (status, raised_at DESC);
CREATE INDEX alerts_household_raised_idx ON alerts (household_id, raised_at DESC);

