#!/bin/bash

sql_port=15432
http_port=8888

docker pull cockroachdb/cockroach:latest

docker run -d --name=cockroachdb \
  -p $sql_port:26257 -p $http_port:8080 \
  cockroachdb/cockroach:latest \
  start-single-node --insecure

# Then, we can connect:
# psql "postgresql://root@localhost:15432/defaultdb"

