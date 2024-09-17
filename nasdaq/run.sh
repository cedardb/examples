#!/bin/bash
set -x

docker stop cedardb_nasdaq
docker run --rm -p 5432:5432 -v .:/nasdaq --name=cedardb_nasdaq cedardb &
sleep 2
docker exec -it cedardb_nasdaq psql -h /tmp -U postgres -c "create user client superuser; alter user client with password 'client'; create database client;"
./bin/NasdaqDriver /nasdaq/data/ data/
