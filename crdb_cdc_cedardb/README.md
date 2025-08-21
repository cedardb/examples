# Demo showing the use of CockroachDB changefeeds (CDC) with CedarDB

**Offload the analytical query workload to CedarDB!**

Since this demo is meant to illustrate the use of CockroachDB changefeeds, it uses the
TPC-C demo app built into CockroachDB.  The entity relationship diagram (ERD) is shown
below.  Here are a couple of handy references:

- [Cockroach TPC-C Workload](https://www.cockroachlabs.com/docs/stable/cockroach-workload#tpcc-workload)
- [CDC into a webhook sink](https://www.cockroachlabs.com/docs/stable/changefeed-examples#create-a-changefeed-connected-to-a-webhook-sink)
- [GitHub repo](https://github.com/cockroachlabs/cdc-webhook-sink-test-server) for Go webhook sink which
  was the inspiration for the code in this repo

![TPC-C schema](./tpcc_erd.png)

## CockroachDB

Start up CockroachDB in Docker:

```bash
$ docker pull cockroachdb/cockroach:latest

$ docker run -d --name=cockroachdb \
  -p 26257:26257 -p 8888:8080 \
  cockroachdb/cockroach:latest \
  start-single-node --insecure
```

You can now access CockroachDB using a SQL client:
```bash
$ psql "postgresql://root@localhost:26257/defaultdb?sslmode=disable"
```

Initialize the TPC-C workload, per the docs referenced above:

```bash
$ cockroach workload init tpcc "postgresql://root@localhost:26257/tpcc?sslmode=disable"
```

## CedarDB

Start up CedarDB in Docker:

```bash
$ docker pull cedardb/cedardb

$ data_dir="$HOME/CedarDB/data"

$ mkdir -p $data_dir

$ docker run -d --rm -p 5432:5432 \
  -v $data_dir:/var/lib/cedardb/data \
  -e CEDAR_PASSWORD=postgres \
  --name cedardb cedardb/cedardb
```

Access CedarDB via a SQL client:
```bash
$ psql "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable"
```

Once that schema exists in CockroachDB, we can see what it looks like and
replicate it in CedarDB.  Note the absence of foreign key constraints here as
the intent is to use CedarDB not as the system of record but as the analytical
query engine.  Create the TPC-C schema within the `postgres` database in
CedarDB:

```bash
$ psql "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable" < tpcc_ddl_cedardb.sql
```

## Start the CDC webhook endpoint

* Build the Docker image:
```bash
$ ./docker_build_webhook.sh
```

* Start the Docker image:
```bash
$ ./docker_run_webhook.sh
```

## CockroachDB

This is the syntax for creating the changefeeds.  Note that, in the URLs here,
there is an ordered list of the primary key components for each table, where
the elements are separated by comma (`,`).  The following commands are executed
via a SQL client (`psql`, etc.) connected to CockroachDB.

```bash
$ psql "postgresql://root@localhost:26257/tpcc?sslmode=disable"
```

Enable rangefeeds, which is a prerequisite for using changefeeds:

```sql
SET CLUSTER SETTING kv.rangefeed.enabled = true;
```

Create the changefeeds:

```sql
CREATE CHANGEFEED FOR TABLE public.warehouse
INTO 'webhook-https://host.docker.internal:8443/cdc/w_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.district
INTO 'webhook-https://host.docker.internal:8443/cdc/d_w_id,d_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.customer
INTO 'webhook-https://host.docker.internal:8443/cdc/c_w_id,c_d_id,c_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.history
INTO 'webhook-https://host.docker.internal:8443/cdc/h_w_id,rowid?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public."order"
INTO 'webhook-https://host.docker.internal:8443/cdc/o_w_id,o_d_id,o_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.new_order
INTO 'webhook-https://host.docker.internal:8443/cdc/no_w_id,no_d_id,no_o_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.item
INTO 'webhook-https://host.docker.internal:8443/cdc/i_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.stock
INTO 'webhook-https://host.docker.internal:8443/cdc/s_w_id,s_i_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.order_line
INTO 'webhook-https://host.docker.internal:8443/cdc/ol_w_id,ol_d_id,ol_o_id,ol_number?insecure_tls_skip_verify=true'
WITH updated;
```

If / when necessary, all changefeeds can be canceled using this SQL command (**don't do this now**):

```sql
CANCEL JOBS (
  SELECT job_id
  FROM [SHOW JOBS]
  WHERE job_type = 'CHANGEFEED' AND status = 'running'
);
```

## Start the TPC-C app

```bash
$ cockroach workload run tpcc "postgresql://root@localhost:26257/tpcc?sslmode=disable"
```

## With this app running, make some observations

- We should be able to see the `SELECT COUNT(*) FROM table_name;` increasing in CedarDB for
  each of the TPC-C tables.
- Run an interesting analytical query in CockroachDB, then in CedarDB and
  compare the results and the runtimes:

```sql
WITH lines AS (
  SELECT ol.ol_i_id, ol.ol_supply_w_id AS sup_w, SUM(ol.ol_quantity) AS qty,
         SUM(ol.ol_amount) AS revenue,
         AVG(ol.ol_amount / NULLIF(ol.ol_quantity,0)) AS avg_price,
         SUM(CASE WHEN ol.ol_supply_w_id <> ol.ol_w_id THEN 1 ELSE 0 END)*1.0/COUNT(*) AS remote_ratio
  FROM order_line ol
  GROUP BY 1,2
),
stock_view AS (
  SELECT s.s_i_id, s.s_w_id, s.s_quantity, s.s_order_cnt, s.s_remote_cnt
  FROM stock s
)
SELECT i.i_id, i.i_name,
       SUM(l.qty) AS total_qty,
       SUM(l.revenue) AS total_rev,
       AVG(l.avg_price)::NUMERIC(9, 3) AS avg_unit_price,
       AVG(l.remote_ratio)::NUMERIC(9, 3) AS avg_remote_ratio,
       SUM(s.s_quantity) AS total_stock_left,
       SUM(s.s_remote_cnt) AS total_remote_cnt
FROM item i
JOIN lines l       ON l.ol_i_id=i.i_id
JOIN stock_view s  ON s.s_i_id=i.i_id AND s.s_w_id=l.sup_w
GROUP BY i.i_id, i.i_name
ORDER BY total_rev DESC
LIMIT 10;
```

Your results should look something like this:

```
 i_id  |          i_name          | total_qty | total_rev | avg_unit_price | avg_remote_ratio | total_stock_left | total_remote_cnt
-------+--------------------------+-----------+-----------+----------------+------------------+------------------+------------------
 36654 | FzXEIQH0c7FD8xD          |        30 |  44378.80 |       1479.293 |            0.000 |               39 |                0
 54211 | IQH0c7FD8xDohfcYp4y27xlV |        35 |  42686.32 |       1219.609 |            0.000 |               21 |                0
 76188 | sPyFzXEIQH0c7FD8x        |        30 |  41807.91 |       1393.597 |            0.000 |               44 |                0
 13317 | H0c7FD8xDohfcYp4y27xlVI1 |        35 |  39809.22 |       1137.406 |            0.000 |               19 |                0
 71380 | 0c7FD8xDohfcYp4y27xlVI1  |        35 |  39326.28 |       1123.608 |            0.000 |               23 |                0
 68615 | EUFjQdsPyFzXEIQH0c       |        35 |  38471.65 |       1099.190 |            0.000 |               85 |                0
 38666 | VI1TeoqmZLaFIDQfa9an     |        45 |  38300.99 |        851.133 |            0.000 |               92 |                0
 49780 | dsPyFzXEIQH0c7FD8xDoh    |        35 |  37906.87 |       1083.053 |            0.000 |               87 |                0
 48364 | IQH0c7FD8xDohfcYp4y2     |        30 |  37834.50 |       1261.150 |            0.000 |               78 |                0
 78379 | TRSt3XVr7REUFjQd         |        20 |  37379.42 |       1868.971 |            0.000 |               80 |                0
(10 rows)

Time: 2573.452 ms (00:02.573)
tpcc=> select version();
                                                    version
---------------------------------------------------------------------------------------------------------------
 CockroachDB CCL v25.3.0 (aarch64-unknown-linux-gnu, built 2025/08/14 18:25:20, go1.23.7 X:nocoverageredesign)
(1 row)
```

```
 i_id  |          i_name          | total_qty | total_rev | avg_unit_price | avg_remote_ratio | total_stock_left | total_remote_cnt
-------+--------------------------+-----------+-----------+----------------+------------------+------------------+------------------
 71380 | 0c7FD8xDohfcYp4y27xlVI1  |        30 |  39326.28 |       1310.876 |            0.000 |               23 |                0
 64785 | 4y27xlVI1TeoqmZLaFIDQfa9 |        25 |  34190.48 |       1367.619 |            0.000 |               71 |                0
 75623 | xlVI1TeoqmZLaFIDQ        |        25 |  33484.98 |       1339.399 |            0.000 |               82 |                0
 76188 | sPyFzXEIQH0c7FD8x        |        20 |  32424.35 |       1621.218 |            0.000 |               44 |                0
 68615 | EUFjQdsPyFzXEIQH0c       |        25 |  31745.46 |       1269.818 |            0.000 |               85 |                0
 21678 | Vr7REUFjQdsPyFzXEIQH0c7  |        20 |  30385.89 |       1519.295 |            0.000 |               90 |                0
 13818 | EIQH0c7FD8xDohfcYp4      |        25 |  29561.92 |       1182.477 |            0.000 |               82 |                0
 22243 | zXEIQH0c7FD8xDohfcYp     |        35 |  29418.78 |        840.537 |            0.000 |               40 |                0
 11983 | t3XVr7REUFjQdsPy         |        25 |  29094.34 |       1163.774 |            0.000 |               63 |                0
  9089 | Vr7REUFjQdsPyFzX         |        15 |  28808.95 |       1920.597 |            0.000 |               62 |                0
(10 rows)

Time: 38.111 ms
postgres=# select version();
                    version
------------------------------------------------
 PostgreSQL 16.3 compatible CedarDB v2025-08-20
(1 row)
```

