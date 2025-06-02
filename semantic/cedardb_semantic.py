#!/usr/bin/env python3

import re, sys, os, time
import logging
import psycopg
from psycopg_pool import ConnectionPool
from flask import Flask, request, Response
import json
import base64
import requests
from fastembed import TextEmbedding
import resource, platform
import nltk
from functools import lru_cache

# Attempt to catch onnxruntime exceptions
from onnxruntime.capi.onnxruntime_pybind11_state import RuntimeException

CHARSET = "utf-8"

VECTOR_DIM = 384 # Fastembed

print() # Clear previous messages

# Set a memory limit (if running on Linux)
if platform.system() == "Linux":
  mem_limit_mb = int(os.environ.get("MEMORY_LIMIT_MB", "4096"))
  print("mem_limit_mb: {} (set via 'export MEMORY_LIMIT_MB=4096')".format(mem_limit_mb))
  rsrc = resource.RLIMIT_DATA
  mem_limit_bytes = mem_limit_mb * (1 << 20)
  resource.setrlimit(rsrc, (mem_limit_bytes, mem_limit_bytes))
else:
  print("Not on Linux; not setting a memory limit")

max_chunks = int(os.environ.get("MAX_CHUNKS", "256"))
print("max_chunks: {} (set via 'export MAX_CHUNKS=128', a value of '0' imposes no limit)".format(max_chunks))

min_sentence_len = int(os.environ.get("MIN_SENTENCE_LEN", "8"))
print("min_sentence_len: {} (set via 'export MIN_SENTENCE_LEN=12')".format(min_sentence_len))

n_threads = int(os.environ.get("N_THREADS", "4"))
print("n_threads: {} (set via 'export N_THREADS=10')".format(n_threads))

# This applies to the LRU cache for the query string to embedding function
cache_size = int(os.environ.get("CACHE_SIZE", "1024"))
print("cache_size: {} (set via 'export CACHE_SIZE=1024')".format(cache_size))

log_level = os.environ.get("LOG_LEVEL", "WARN").upper()
logging.basicConfig(
  level=log_level
  , format="[%(asctime)s %(threadName)s] %(message)s"
  , datefmt="%m/%d/%Y %I:%M:%S %p"
)
print("Log level: {} (export LOG_LEVEL=[DEBUG|INFO|WARN|ERROR] to alter log verbosity)".format(log_level))

db_url = os.getenv("DB_URL")
if db_url is None:
  print("DB_URL must be set")
  sys.exit(1)

db_url = re.sub(r"^postgres(ql)?", "postgresql", db_url)

t0 = time.time()
embed_model = TextEmbedding()
et = time.time() - t0
logging.info("TextEmbedding model ready: {:.2f} s".format(et))

t0 = time.time()
nltk.download("punkt_tab")
et = time.time() - t0
logging.info("NLTK ready: {:.2f} s".format(et))

ddl_t1 = """
CREATE TABLE text_embed
(
  uri STRING NOT NULL
  , chunk_num INT NOT NULL
  , chunk STRING NOT NULL
  , embedding VECTOR ({})
  , PRIMARY KEY (uri, chunk_num)
);
""".format(VECTOR_DIM)

sql_check_exists = """
SELECT COUNT(*) n FROM information_schema.tables
WHERE
  table_schema = 'public'
  AND table_name = 'text_embed';
"""

pool = ConnectionPool(db_url, open=True)

def run_ddl(ddl):
  with pool.connection() as conn:
    conn.execute(ddl)
    conn.commit()

def setup_db():
  logging.info("Checking whether text_embed table exists")
  n_rows = 0
  with pool.connection() as conn:
    rs = conn.execute(sql_check_exists)
    for row in rs:
      n_rows = row[0]
  table_exists = (n_rows == 1)
  if not table_exists:
    logging.info("Creating table ...")
    run_ddl(ddl_t1)
    logging.info("OK")
  else:
    logging.info("text_embed table already exists")

sql_inserts = """
INSERT INTO text_embed (uri, chunk_num, chunk, embedding)
VALUES (%(uri)s, %(chunk_num)s, %(chunk)s, %(embedding)s);
"""

def index_text(conn, uri, text):
  te_rows = []
  ca_rows = []
  n_chunk = 0
  s_list = []
  for s in nltk.sent_tokenize(text):
    s = s.strip()
    if (len(s) >= min_sentence_len):
      s_list.append(s)
    if max_chunks > 0 and len(s_list) == max_chunks:
      break
  logging.info("n_chunks = {}".format(len(s_list)))
  t0 = time.time()
  try:
    embed_list = list(embed_model.embed(s_list)) # Memory leaks here
  except RuntimeException as e:
    logging.warning(e)
    return Response(str(e), status=500, mimetype="text/plain")
  et = time.time() - t0
  logging.info("Time to generate embeddings(): {:.2f} ms".format(et * 1000))
  for i in range(0, len(s_list)):
    row_map = {
      "uri": uri
      , "chunk_num": n_chunk
      , "chunk": s_list[i]
      , "embedding": embed_list[i].tolist()
    }
    te_rows.append(row_map)
    n_chunk += 1
  t0 = time.time()
  try:
    with conn.cursor() as cur:
      cur.executemany(sql_inserts, te_rows)
    conn.commit()
  except Exception as e:
    return Response(str(e), status=400, mimetype="text/plain")
  et = time.time() - t0
  logging.info("DB INSERT time: {:.2f} ms".format(et * 1000))
  return Response("OK", status=200, mimetype="text/plain")

def index_file(in_file):
  text = ""
  with open(in_file, mode="rt") as f:
    for line in f:
      text += line
  in_file = re.sub(r"\./", '', in_file) # Trim leading '/'
  with pool.connection() as conn:
    rv = index_text(conn, in_file, text)
  return rv

# Clean any special chars out of text
def clean_text(text):
  return re.sub(r"['\",{}]", "", text)

# Decode a base64 encoded value to a UTF-8 string
def decode(b64):
  b = base64.b64decode(b64)
  return b.decode(CHARSET).strip()

sql_search = """
SELECT uri, 1 - (embedding <=> (%s)::VECTOR) sim, chunk
FROM text_embed
ORDER BY sim DESC
LIMIT %s
"""

@lru_cache(maxsize=cache_size)
def get_embed_for_search(query_string):
  embed_list = list(embed_model.embed([query_string]))
  return embed_list[0]

# Arg: search terms
# Returns: list of {"uri": uri, "score": sim, "token": token, "chunk": chunk}
def search(conn, terms, limit):
  rv = []
  q = ' '.join(terms)
  #embed_list = list(embed_model.embed([q]))
  #embed = embed_list[0]
  embed = get_embed_for_search(q)
  logging.info("Query string: '{}'".format(q))
  t0 = time.time()
  rs = conn.execute(sql_search, (embed.tolist(), limit))
  if rs is not None:
    for row in rs:
      (uri, sim, chunk) = row
      if len(chunk) > 96:
        chunk = chunk[0:96] + " ..."
      rv.append({"uri": uri, "score": float(sim), "chunk": chunk})
  et = time.time() - t0
  logging.info("SQL query time: {:.2f} ms".format(et * 1000))
  return rv

app = Flask(__name__)

#
# Search / query
#
# EXAMPLE (with a limit of 5 results):
#
#   curl http://localhost:18080/search/$( echo -n 'how does the CedarDB "asof join" work' | base64 )/5
#
@app.route("/search/<q_base_64>/<int:limit>")
def do_search(q_base_64, limit):
  q = decode(q_base_64)
  q = clean_text(q)
  with pool.connection() as conn:
    rv = search(conn, q.split(), limit)
  return Response(json.dumps(rv), status=200, mimetype="application/json")

@app.route("/index", methods=["POST"])
def do_index():
  rv = Response("OK", status=200, mimetype="text/plain")
  data = request.get_json(force=True)
  with pool.connection() as conn:
    rv = index_text(conn, data["uri"], data["text"])
  return rv

@app.route("/health", methods=["GET"])
def health():
  return Response("OK", status=200, mimetype="text/plain")

# main()
setup_db()
port = int(os.getenv("FLASK_PORT", 1999))
from waitress import serve
serve(app, host="0.0.0.0", port=port, threads=n_threads)

