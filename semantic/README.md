# Semantic Search in CedarDB

This semantic search app uses CedarDB's `pgvector` support to provide semantic search over texts
which are ingested via this app.

## Steps to get everything running:

```
source env.sh
./docker_build_image.sh
./gen_doc_blog_url_list.sh > list.txt
for url in $( cat ./list.txt )
do
  ./index_url.sh $url
done
./search_client.sh "Code Generation"
```

## Index documents from the local filesystem

* Start a simple HTTP server from within the directory containing your documents:
```
cd $HOME/wiki_pages/
python3 -m http.server 8000
```

* Get a list of documents in that directory, and use that list to drive the indexing loop:
```
for page in $HOME/wiki_pages/*.txt
do
  page=$( basename $page)
  url="http://localhost:8000/$page"
  ./index_url.sh $url
done
```

## TODO

* Finish this README
* Consider some data sets(?)

