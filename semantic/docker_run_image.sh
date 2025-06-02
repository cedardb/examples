#!/bin/bash

port=1999 # You point your client at this port

. ./docker_include.sh
arch=$( uname -p )
img_name="cedardb-semantic-$arch"
img="$docker_id/$img_name"

# Environment variables defined here
. ./env.sh

#docker pull $img:$tag

docker run \
  -e DB_URL -e FLASK_PORT -e FLASK_HOST -e LOG_LEVEL -e N_THREADS -e MIN_SENTENCE_LEN \
  -e TOKENIZERS_PARALLELISM -e MEMORY_LIMIT_MB -e MAX_CHUNKS \
  --publish $port:$FLASK_PORT mgoddard/cedardb-semantic-arm

# --publish $port:$FLASK_PORT $img:$tag

