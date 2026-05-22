#!/bin/bash

# Using a random port which isn't likely to be taken by someone else
export DATABASE_URL="postgresql://postgres:postgres@localhost:26257/postgres?sslmode=require"

# Simulator
export HG_STORAGE_SAMPLER_INTERVAL="5s"
export HG_INGEST_BATCH="50000"
export HG_INGESTORS="10"

# Server
export HG_SSE_INTERVAL="1s"
export HG_ALERTS_REFRESH="5s"
export HG_DRILLDOWN_REFRESH="5s"
export HG_STATS_REFRESH="1s"
export HG_STORAGE_REFRESH="2s"

# Small run, for a little laptop:
#nohup ./out/simulator -households=30000 -devices-per-household=10 -rate=2500 -hz=10 -writers=4 >> simulator.log 2>&1 </dev/null &

# Large run, for a server with 192 cores, 384 GB RAM, lots of disk space
nohup ./out/simulator -households=200000 -devices-per-household=12 -rate=1750000 -hz=20 -writers=64 >> simulator.log 2>&1 </dev/null &

# Server startup
nohup ./out/server -addr=:18080 >> server.log 2>&1 </dev/null &

# Tail the logs:
# tail -50f simulator.log

