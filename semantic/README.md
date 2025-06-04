# Semantic Search in CedarDB

This semantic search app uses CedarDB's `pgvector` support to provide semantic search over texts
which are ingested via this app.


Steps to get everything running:


```
source env.sh
./docker_build_image.sh
./gen_doc_blog_url_list.sh > list.txt
./index_url.py $( cat ./list.txt )
./search_client.sh "Code Generation"
```

## TODO

* Finish this README
* Consider some data sets(?)
* Store the `Last-Modified: Wed, 28 May 2025 17:06:31 GMT` for each distinct URI so that repeated
  indexing operations on the same (URI, TIMESTAMP) will be no-ops.  Useful for re-indexing a set
  of documents when which ones have changes is unknown.
```
$ curl -I https://cedardb.com/docs/references/advanced/prepare/
HTTP/1.1 200 OK
Server: nginx/1.24.0 (Ubuntu)
Date: Tue, 03 Jun 2025 20:35:02 GMT
Content-Type: text/html
Content-Length: 167466
Last-Modified: Wed, 28 May 2025 17:06:31 GMT
Connection: keep-alive
ETag: "68374297-28e2a"
Accept-Ranges: bytes
```

