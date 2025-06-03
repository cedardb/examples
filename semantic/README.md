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
* Fix this all up to support Linux as well
* Consider some data sets(?)

