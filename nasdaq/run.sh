#!/bin/bash
killall server
~/path/to/cedardb/server -createdb /opt/dbs/stocks.db &
sleep 2
psql -h /tmp -U postgres -f initdb.sql
client/NasdaqDriver ~/path/to/examples/nasdaq/data/
