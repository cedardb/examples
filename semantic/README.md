# Semantic Search in CedarDB

This semantic search app uses CedarDB's `pgvector` support to provide semantic
search over texts which are ingested via this app.

## Steps to get everything running

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

* Start a simple HTTP server from within a directory containing documents you want to index:
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

## Possible optimization: include a predicate to limit the scope of the scan

In this next output, the SQL query contained in the [file](./example_search.sql)
**doesn't contain a predicate** at all; it merely scores all rows and returns the top-N
based on their cosine distance from the embedding vector generated from the query
string:
```
[11:57:56 semantic]$ PGPASSWORD=postgres psql -h localhost -U postgres -d postgres < ./example_search.sql
Timing is on.
                                                                                plan
---------------------------------------------------------------------------------------------------------------------------------------------------------------------
 🖩 OUTPUT ()                                                                                                                                                        +
 ▼ SORT (In-Memory) (Result Materialized: 40 KB, Result Utilization: 80 %, Peak Materialized: 47 KB, Peak Utilization: 78 %, Card: 5, Estimate: 5, Time: 0 ms (0 %))+
 𝚾 MAP (Card: 323'596, Estimate: 316'837)                                                                                                                           +
 TABLESCAN on text_embed (num IOs: 0, Fetched: 0 B, Card: 323'596, Estimate: 316'837, Time: 26 ms (100 % ***))
(1 row)

Time: 28.543 ms
```

Since this search demo uses documents obtained from URLs specified at indexing time,
it's possible to constrain the number of rows scored using cosine similarity based on
a predicate that includes a string found in the URL.  For example, for documents from
the CedarDB web site (this query is based on one of those documents), we could add a
predicate to match "cedardb" anywhere in the URI column:
```sql
WHERE uri ~* 'cedardb'
```

As shown below, this results in a significant reduction in the number of rows scanned and, hence,
in a faster search.  The effect of this would become more significant when the index contains
a larger data set.

```
[11:57:32 semantic]$ PGPASSWORD=postgres psql -h localhost -U postgres -d postgres < ./example_search.sql
Timing is on.
                                                                                plan
---------------------------------------------------------------------------------------------------------------------------------------------------------------------
 🖩 OUTPUT ()                                                                                                                                                        +
 ▼ SORT (In-Memory) (Result Materialized: 14 KB, Result Utilization: 69 %, Peak Materialized: 16 KB, Peak Utilization: 69 %, Card: 5, Estimate: 5, Time: 0 ms (0 %))+
 𝚾 MAP (Card: 3'795, Estimate: 2'475)                                                                                                                               +
 TABLESCAN on text_embed (num IOs: 0, Fetched: 0 B, Card: 3'795, Estimate: 2'475, Time: 10 ms (100 % ***))
(1 row)

Time: 13.389 ms
```

## TODO

* Finish this README
* Consider some data sets(?)

