# For server
#export DB_URL="postgres://postgres:postgres@127.0.0.1:5432/postgres"
#export DB_URL="postgres://postgres:postgres@cedardb:5432/postgres"
export DB_URL="postgres://postgres:postgres@docker.for.mac.localhost:5432/postgres"
export FLASK_PORT=1999
export FLASK_HOST=localhost
export LOG_LEVEL=INFO
export N_THREADS=10
export MIN_SENTENCE_LEN=8
export TOKENIZERS_PARALLELISM=false
export MEMORY_LIMIT_MB=6144
export MAX_CHUNKS=128

# For client
export MAX_RESULTS=5

