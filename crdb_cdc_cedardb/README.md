# Demo showing the use of CockroachDB changefeeds (CDC) with CedarDB

  **Offload your analytical query workload to CedarDB!**

Since this demo illustrates the use of CockroachDB changefeeds to sync data to
CedarDB, it uses the **TPC-C** _workload_ which is built into CockroachDB.  The
TPCC-C entity relationship diagram (ERD) is shown below.  This demo is based on
Docker, so you'll need to have that installed.

Here are a couple of handy references:

- [Cockroach TPC-C Workload](https://www.cockroachlabs.com/docs/stable/cockroach-workload#tpcc-workload)
- [CDC into a webhook sink](https://www.cockroachlabs.com/docs/stable/changefeed-examples#create-a-changefeed-connected-to-a-webhook-sink)
- [GitHub repo](https://github.com/cockroachlabs/cdc-webhook-sink-test-server) for the Go webhook sink which
  was the inspiration for [the code](./main.go) in this repo

![TPC-C schema](./tpcc_erd.png)

## CockroachDB

- Start up CockroachDB in Docker:

```bash
$ docker pull cockroachdb/cockroach:latest
$ docker run -d --name=cockroachdb \
  -p 26257:26257 -p 8888:8080 \
  cockroachdb/cockroach:latest \
  start-single-node --insecure
```

- You can now access CockroachDB using its built-in SQL client :
```bash
$ docker exec -it $( docker ps -q --filter "ancestor=cockroachdb/cockroach:latest" ) cockroach sql --url "postgresql://root@localhost:26257/defaultdb?sslmode=disable"
```

- Initialize the TPC-C workload, per the docs referenced above:

```bash
$ docker exec -it $( docker ps -q --filter "ancestor=cockroachdb/cockroach:latest" ) cockroach workload init tpcc "postgresql://root@localhost:26257/tpcc?sslmode=disable"
```

This will result in IMPORT statements running for each of the 9 tables.  The post-import row
counts can be seen by logging in via that SQL client (but using the `tpcc` database this time):
```bash
$ docker exec -it $( docker ps -q --filter "ancestor=cockroachdb/cockroach:latest" ) cockroach sql --url "postgresql://root@localhost:26257/tpcc?sslmode=disable"
```

and then running the `SHOW TABLES` command:
```sql
root@localhost:26257/tpcc> show tables;
  schema_name | table_name | type  | owner | estimated_row_count | locality
--------------+------------+-------+-------+---------------------+-----------
  public      | customer   | table | root  |               30000 | NULL
  public      | district   | table | root  |                  10 | NULL
  public      | history    | table | root  |               30000 | NULL
  public      | item       | table | root  |              100000 | NULL
  public      | new_order  | table | root  |                9000 | NULL
  public      | order      | table | root  |               30000 | NULL
  public      | order_line | table | root  |              299278 | NULL
  public      | stock      | table | root  |              100000 | NULL
  public      | warehouse  | table | root  |                   1 | NULL
(9 rows)
```

Below, we'll use the row count for the `order_line` table as the signal that
the initial replication to CedarDB is complete.

## CedarDB

- Start up CedarDB in Docker, setting `data_dir` to a suitable location:

```bash
$ docker pull cedardb/cedardb
$ data_dir="$HOME/CedarDB/data"
$ mkdir -p $data_dir
$ docker run -d --rm -p 5432:5432 \
  -v $data_dir:/var/lib/cedardb/data \
  -e CEDAR_PASSWORD=postgres \
  --name cedardb cedardb/cedardb
```

- Access CedarDB via the `psql` client built into the Docker image:
```bash
$ docker exec -it $( docker ps -q --filter "ancestor=cedardb/cedardb" ) psql "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable"
```

Now that the TPC-C schema exists in CockroachDB, we can see what it looks like
and create it in CedarDB, prior to starting the CDC feeds.  Note the absence of
foreign key constraints [here](./tpcc_ddl_cedardb.sql) as the intent is to use
CedarDB not as the system of record but as the analytical query engine.

- Create the TPC-C schema within the `postgres` database in CedarDB:

```bash
$ docker exec -i $( docker ps -q --filter "ancestor=cedardb/cedardb" ) psql "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable" < tpcc_ddl_cedardb.sql
```

## Start the CDC webhook endpoint

- Build the Docker image:
```bash
$ ./docker_build_webhook.sh
```

- Start the Docker image:
```bash
$ ./docker_run_webhook.sh
```

## CockroachDB

This example shows the syntax for creating the changefeeds.  Note that each of
the URLs here includes an ordered list of the primary key components for the
table, where the elements are separated by comma (`,`).  The actual SQL to
create these changefeeds is input directly from the `./tpcc_changefeeds.sql`
file in the next step.

```sql
CREATE CHANGEFEED FOR TABLE public.customer
INTO 'webhook-https://host.docker.internal:8443/cdc/c_w_id,c_d_id,c_id?insecure_tls_skip_verify=true'
WITH updated;
```

- Create the changefeeds:

```bash
$ docker exec -i $( docker ps -q --filter "ancestor=cockroachdb/cockroach:latest" ) cockroach sql --url "postgresql://root@localhost:26257/tpcc?sslmode=disable" < tpcc_changefeeds.sql
```

That should result in output that looks like:
```
SET CLUSTER SETTING
job_id
1100416707119644673
job_id
1100416707138813953
[...]
```

- For the initial load, it would be better to export CSV from CockroachDB and
then import it into CedarDB but, for now, we'll watch the row count for the
`order_line` table in CedarDB, waiting for it to hit that **299278** value:

```bash
$ docker exec -it $( docker ps -q --filter "ancestor=cedardb/cedardb" ) psql "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable"
```
```
postgres=# select count(*) from order_line;
 count
--------
 197031
(1 row)

postgres=# \watch 5
Fri Aug 22 19:45:12 2025 (every 5s)

 count
--------
 203926
(1 row)

[...]

Fri Aug 22 19:45:22 2025 (every 5s)

 count
--------
 299278
(1 row)

^C
```

**Done:** we hit the target row count, CTRL-C that commannd, and move forward ...

- **Note:** If/when necessary, all changefeeds can be canceled using this SQL command (**don't do this now**):

```sql
CANCEL JOBS (
  SELECT job_id
  FROM [SHOW JOBS]
  WHERE job_type = 'CHANGEFEED' AND status = 'running'
);
```

## Start the TPC-C app

```bash
$ docker exec -it $( docker ps -q --filter "ancestor=cockroachdb/cockroach:latest" ) cockroach workload run tpcc "postgresql://root@localhost:26257/tpcc?sslmode=disable"
```

## With this app running, make some observations

- We should be able to see the `SELECT COUNT(*) FROM table_name;` increasing in CedarDB for
  each of the TPC-C tables.

- Run an interesting analytical query in CockroachDB, then in CedarDB and
  compare the results and the runtimes (be sure to set `\timing on` in the psql
  client for CedarDB, so it reports the runtime):

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

