#!/bin/bash

port=1999 # You point your client at this port

. ./docker_include.sh
img="$docker_id/$img_name"

# Environment variables defined here
. ./env.sh

#docker pull $img:$tag

network="--network=\"host\"" # Linux
if [[ "Darwin" == $( uname -s ) ]]
then
  network=""
fi

docker run \
  -e DB_URL -e FLASK_PORT -e FLASK_HOST -e LOG_LEVEL -e N_THREADS -e MIN_SENTENCE_LEN \
  -e TOKENIZERS_PARALLELISM -e MEMORY_LIMIT_MB -e MAX_CHUNKS -e MIN_SIMILARITY \
  --publish $port:$FLASK_PORT \
  $network \
  $img
# $img:$tag

