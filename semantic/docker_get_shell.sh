#!/bin/bash

container=$( docker ps | grep crdb-embeddings | grep -v grep | awk '{print $1}' )
docker exec -it $container bash

