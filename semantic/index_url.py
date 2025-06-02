#!/usr/bin/env python3

import requests
from bs4 import BeautifulSoup
import sys, os, re
import time

host = os.environ.get("FLASK_HOST", "localhost")
port = os.environ.get("FLASK_PORT", "18080")
url = "http://{}:{}/index".format(host, port)

if len(sys.argv) < 2:
  print("Usage: {} URI_to_index [URI_2 ...]\n".format(sys.argv[0]))
  sys.exit(1)

def read_url(url):
  text = None
  try:
    response = requests.get(url)
    response.raise_for_status()
  except requests.RequestException as e:
    sys.stderr.write(f"Error fetching URL {url}: {e}\n")
    return text
  soup = BeautifulSoup(response.text, 'html.parser')
  # Remove script and style elements
  for element in soup(['script', 'style']):
    element.decompose()
  text = soup.get_text(separator=' ', strip=True)
  return text

def read_file(in_file):
  rv = ""
  with open(in_file, mode="rb") as f:
    try:
      rv = f.read().decode(errors="replace")
    except UnicodeDecodeError as e:
      print(e)
      rv = None
  return rv

for doc_uri in sys.argv[1:]:
  t0 = time.time()
  print("URI: {}".format(doc_uri))
  doc_text = None
  if doc_uri.startswith("http"):
    doc_text = read_url(doc_uri)
  else:
    doc_text = read_file(doc_uri)
  if doc_text is None:
    print("FAILED to extract text: {}".format(doc_uri))
    continue
  doc_uri = re.sub(r"^[\./]+", '', doc_uri) # Used for local filesystem path
  req = requests.post(url, json = { "uri": doc_uri, "text": doc_text })
  et = time.time() - t0
  print("{} (t = {:.3f} ms)".format("SUCCESS" if req.status_code == 200 else "FAILED: " + req.content.decode("utf-8"), et*1000))

