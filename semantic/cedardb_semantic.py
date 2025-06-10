#!/usr/bin/env python3

import re, sys, os, time
import logging
import psycopg
from psycopg_pool import ConnectionPool
from flask import Flask, request, Response
import json
import base64
import requests
from bs4 import BeautifulSoup
from fastembed import TextEmbedding
import resource, platform
import nltk
from functools import lru_cache
import hashlib, hmac
import random

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
CREATE TABLE text_embed_freshness
(
  uri STRING NOT NULL
  , sha256 VARCHAR(64)
  , PRIMARY KEY (uri)
);
"""

ddl_t2 = """
CREATE TABLE text_embed
(
  uri STRING NOT NULL REFERENCES text_embed_freshness (uri)
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
    logging.info("Creating tables ...")
    run_ddl(ddl_t1)
    run_ddl(ddl_t2)
    logging.info("OK")
  else:
    logging.info("text_embed table already exists")

sql_inserts = """
INSERT INTO text_embed (uri, chunk_num, chunk, embedding)
VALUES (%(uri)s, %(chunk_num)s, %(chunk)s, %(embedding)s)
ON CONFLICT (uri, chunk_num) DO UPDATE
SET embedding = EXCLUDED.embedding;
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
  embed_list = list(embed_model.embed(s_list)) # Memory leaks here
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
  with conn.cursor() as cur:
    cur.executemany(sql_inserts, te_rows)
  conn.commit()
  et = time.time() - t0
  logging.info("DB INSERT time: {:.2f} ms".format(et * 1000))
  return n_chunk

# Clean any special chars out of text
def clean_text(text):
  return re.sub(r"['\",{}]", "", text)

# Decode a base64 encoded value to a UTF-8 string
def decode(b64):
  b = base64.urlsafe_b64decode(b64)
  return b.decode(CHARSET).strip()

sql_search = """
SELECT uri, 1 - (embedding <=> (%s)::VECTOR) sim, chunk, chunk_num
FROM text_embed
ORDER BY sim DESC
LIMIT %s
"""

sql_constrained_search = """
SELECT uri, 1 - (embedding <=> (%s)::VECTOR) sim, chunk, chunk_num
FROM text_embed
WHERE uri ~* %s
ORDER BY sim DESC
LIMIT %s
"""

sql_insert_fresh = """
INSERT INTO text_embed_freshness (uri, sha256)
VALUES (%s, %s)
ON CONFLICT (uri) DO UPDATE
SET sha256 = EXCLUDED.sha256;
"""

sql_uri_fresh = "SELECT sha256 FROM text_embed_freshness WHERE uri = %s;"

sql_delete_old = """
DELETE FROM text_embed
WHERE uri = %s AND chunk_num > %s;
"""

@lru_cache(maxsize=cache_size)
def get_embed_for_search(query_string):
  embed_list = list(embed_model.embed([query_string]))
  return embed_list[0]

# Arg: search terms
# Returns: list of {"uri": uri, "score": sim, "token": token, "chunk": chunk}
def search(conn, terms, limit, url_constraint):
  rv = []
  q = ' '.join(terms)
  embed = get_embed_for_search(q)
  logging.info("Query string: '{}', URL constraint: {}".format(q, url_constraint))
  t0 = time.time()
  rs = None
  # FIXME: this actually slows things down considerably. Why?
  if url_constraint is not None:
    rs = conn.execute(sql_constrained_search, (embed.tolist(), url_constraint, limit))
  else:
    rs = conn.execute(sql_search, (embed.tolist(), limit))
  if rs is not None:
    for row in rs:
      (uri, sim, chunk, chunk_num) = row
      if len(chunk) > 96:
        chunk = chunk[0:96] + " ..."
      rv.append({"uri": uri, "score": float(sim), "chunk": chunk, "chunk_num": int(chunk_num)})
  et = time.time() - t0
  logging.info("SQL query time: {:.2f} ms".format(et * 1000))
  return rv

# Given a URL, return the SHA256 hash of a combination of its headers, or None if there was an error
hdr_candidates = ["Last-Modified", "Content-Length", "Etag"]
hdr_pat = re.compile('(' + '|'.join(hdr_candidates) + ')', re.IGNORECASE)
def freshness_token(url):
  rv = None
  response = requests.head(url, allow_redirects=True)
  if 200 == response.status_code:
    hdrs = []
    for k, v in response.headers.items():
      if hdr_pat.match(k):
        hdrs.append(v)
    if len(hdrs) == 0:
      hdrs.append(str(random.random())) # Will trigger unconditional refresh of this URL
    rv = hashlib.sha256(",".join(hdrs).encode("utf-8")).hexdigest()
  else:
    logging.warning("HEAD of URL '{}' failed: {}".format(url, response.status_code))
  return rv

# Read a URL and return its text or None if there was an error
def read_url(url):
  rv = None
  try:
    response = requests.get(url)
    response.raise_for_status()
  except requests.RequestException as e:
    logging.warning(f"Error fetching URL {url}: {e}\n")
    return rv
  soup = BeautifulSoup(response.text, 'html.parser')
  # Remove script and style elements
  for element in soup(['script', 'style']):
    element.decompose()
  rv = soup.get_text(separator=' ', strip=True)
  return rv

# Initialize Flask
app = Flask(__name__)

#
# Search (here, with a limit of 5 results):
#
#   curl http://localhost:18080/search/$( echo -n 'how does the CedarDB "asof join" work' | base64 )/5
#
@app.route("/search/<q_b64>/<int:limit>")
@app.route("/search/<q_b64>/<int:limit>/<url_constraint_b64>")
def do_search(q_b64, limit, url_constraint_b64=None):
  q = decode(q_b64)
  url_constraint = None
  if url_constraint_b64 is not None:
    url_constraint = decode(url_constraint_b64)
  q = clean_text(q)
  with pool.connection() as conn:
    rv = search(conn, q.split(), limit, url_constraint)
  return Response(json.dumps(rv), status=200, mimetype="application/json")

#
# Add a new document to the index, using its URL:
#
#   time curl -s http://$FLASK_HOST:$FLASK_PORT/index/$( echo -n "$1" | base64 )
#
@app.route("/index/<url_base_64>", methods=["GET"])
def do_index_url(url_base_64):
  rv = Response("OK", status=200, mimetype="text/plain")
  url = decode(url_base_64)
  if len(url) == 0: # Empty URL value
    return rv
  url_sha = freshness_token(url)
  db_sha = None
  with pool.connection() as conn:
    rs = conn.execute(sql_uri_fresh, (url,))
    row = rs.fetchone()
    if row is not None:
      db_sha = row[0]
  # URI there, sha256 is identical
  logging.debug("db_sha: {}, url_sha: {}".format(db_sha, url_sha))
  if db_sha == url_sha:
    return rv
  # URI not there at all or sha256 doesn't match
  # Fetch URL and update the DB
  txt = read_url(url)
  with pool.connection() as conn:
    # UPSERT that (url, url_sha) row first since there is an FK constraint
    with conn.cursor() as cur:
      cur.execute(sql_insert_fresh, (url, url_sha))
    conn.commit()
    last_chunk_num = index_text(conn, url, txt)
    # Remove any rows where chunk_num > last_chunk_num
    if db_sha is not None and db_sha != url_sha:
      with conn.cursor() as cur:
        cur.execute(sql_delete_old, (url, last_chunk_num))
      conn.commit()
  return rv

# TODO: remove as this is not currently used
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

