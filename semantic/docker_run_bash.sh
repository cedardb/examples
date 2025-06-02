#!/bin/bash

# EDIT THESE TO SUIT YOUR DEPLOYMENT
crdb_certs="$HOME/certs"
export DB_URL="postgres://test_role:123abc@host.docker.internal:26257/defaultdb?sslmode=require&sslrootcert=$crdb_certs/ca.crt"
port=1972 # You point your client at this port on localhost, so 'export FLASK_PORT=1999' in env.sh

# THESE ARE PROBABLY ALRIGHT AS-IS
. ./docker_include.sh
arch=$( uname -p )
img_name="crdb-embeddings-$arch"
img="$docker_id/$img_name"

export FLASK_PORT=18080
export FLASK_HOST=localhost
export LOG_LEVEL=INFO
export N_THREADS=10
export MIN_SENTENCE_LEN=8
export N_CLUSTERS=1536
export TRAIN_FRACTION=0.15
export MODEL_FILE=/tmp/model.pkl
export MODEL_FILE_URL="https://storage.googleapis.com/crl-goddard-text/model_Fastembed_1536.pkl"
export BATCH_SIZE=1024
export KMEANS_VERBOSE=2
export KMEANS_MAX_ITER=100
export SECRET="TextWithNoSpecialChars"
export BLOB_STORE_KEEP_N_ROWS=3
export TOKENIZERS_PARALLELISM=false
export MEMORY_LIMIT_MB=4096
export SKIP_KMEANS=false

docker pull $img:$tag

docker run --entrypoint /bin/bash \
  -e DB_URL -e FLASK_PORT -e FLASK_HOST -e LOG_LEVEL -e N_THREADS -e MIN_SENTENCE_LEN \
  -e N_CLUSTERS -e TRAIN_FRACTION -e MODEL_FILE -e MODEL_FILE_URL -e BATCH_SIZE -e KMEANS_VERBOSE \
  -e KMEANS_MAX_ITER -e SKIP_KMEANS -e SECRET -e BLOB_STORE_KEEP_N_ROWS -e MEMORY_LIMIT_MB \
  --publish $port:$FLASK_PORT $img \
  -c 'sleep 3600'

