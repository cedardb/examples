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

Once that schema exists in CockroachDB, we can see what it looks like and
replicate it in CedarDB.  Note the absence of foreign key constraints here as
the intent is to use CedarDB not as the system of record but as the analytical
query engine.  Here's the schema you need to create within the `postgres`
database in CedarDB:

```sql
CREATE TABLE public.warehouse
(
	w_id INT8 NOT NULL,
	w_name VARCHAR(10) NOT NULL,
	w_street_1 VARCHAR(20) NOT NULL,
	w_street_2 VARCHAR(20) NOT NULL,
	w_city VARCHAR(20) NOT NULL,
	w_state CHAR(2) NOT NULL,
	w_zip CHAR(9) NOT NULL,
	w_tax DECIMAL(4,4) NOT NULL,
	w_ytd DECIMAL(12,2) NOT NULL,
	CONSTRAINT warehouse_pkey PRIMARY KEY (w_id)
);

CREATE TABLE public.district
(
	d_id INT8 NOT NULL,
	d_w_id INT8 NOT NULL,
	d_name VARCHAR(10) NOT NULL,
	d_street_1 VARCHAR(20) NOT NULL,
	d_street_2 VARCHAR(20) NOT NULL,
	d_city VARCHAR(20) NOT NULL,
	d_state CHAR(2) NOT NULL,
	d_zip CHAR(9) NOT NULL,
	d_tax DECIMAL(4,4) NOT NULL,
	d_ytd DECIMAL(12,2) NOT NULL,
	d_next_o_id INT8 NOT NULL,
	CONSTRAINT district_pkey PRIMARY KEY (d_w_id, d_id)
);

CREATE TABLE public.customer
(
	c_id INT8 NOT NULL,
	c_d_id INT8 NOT NULL,
	c_w_id INT8 NOT NULL,
	c_first VARCHAR(16) NOT NULL,
	c_middle CHAR(2) NOT NULL,
	c_last VARCHAR(16) NOT NULL,
	c_street_1 VARCHAR(20) NOT NULL,
	c_street_2 VARCHAR(20) NOT NULL,
	c_city VARCHAR(20) NOT NULL,
	c_state CHAR(2) NOT NULL,
	c_zip CHAR(9) NOT NULL,
	c_phone CHAR(16) NOT NULL,
	c_since TIMESTAMP NOT NULL,
	c_credit CHAR(2) NOT NULL,
	c_credit_lim DECIMAL(12,2) NOT NULL,
	c_discount DECIMAL(4,4) NOT NULL,
	c_balance DECIMAL(12,2) NOT NULL,
	c_ytd_payment DECIMAL(12,2) NOT NULL,
	c_payment_cnt INT8 NOT NULL,
	c_delivery_cnt INT8 NOT NULL,
	c_data VARCHAR(500) NOT NULL,
	CONSTRAINT customer_pkey PRIMARY KEY (c_w_id, c_d_id, c_id)
);
CREATE INDEX ON public.customer (c_w_id, c_d_id, c_last, c_first);

CREATE TABLE public.history
(
	rowid UUID NOT NULL DEFAULT gen_random_uuid(),
	h_c_id INT8 NOT NULL,
	h_c_d_id INT8 NOT NULL,
	h_c_w_id INT8 NOT NULL,
	h_d_id INT8 NOT NULL,
	h_w_id INT8 NOT NULL,
	h_date TIMESTAMP NULL,
	h_amount DECIMAL(6,2) NULL,
	h_data VARCHAR(24) NULL,
	CONSTRAINT history_pkey PRIMARY KEY (h_w_id, rowid)
);

CREATE TABLE public."order"
(
	o_id INT8 NOT NULL,
	o_d_id INT8 NOT NULL,
	o_w_id INT8 NOT NULL,
	o_c_id INT8 NULL,
	o_entry_d TIMESTAMP NULL,
	o_carrier_id INT8 NULL,
	o_ol_cnt INT8 NULL,
	o_all_local INT8 NULL,
	CONSTRAINT order_pkey PRIMARY KEY (o_w_id, o_d_id, o_id),
);
CREATE UNIQUE INDEX ON public."order" (o_w_id, o_d_id, o_c_id, o_id);

CREATE TABLE public.new_order
(
	no_o_id INT8 NOT NULL,
	no_d_id INT8 NOT NULL,
	no_w_id INT8 NOT NULL,
	CONSTRAINT new_order_pkey PRIMARY KEY (no_w_id, no_d_id, no_o_id)
);

CREATE TABLE public.item
(
	i_id INT8 NOT NULL,
	i_im_id INT8 NULL,
	i_name VARCHAR(24) NULL,
	i_price DECIMAL(5,2) NULL,
	i_data VARCHAR(50) NULL,
	CONSTRAINT item_pkey PRIMARY KEY (i_id)
);

CREATE TABLE public.stock
(
	s_i_id INT8 NOT NULL,
	s_w_id INT8 NOT NULL,
	s_quantity INT8 NULL,
	s_dist_01 CHAR(24) NULL,
	s_dist_02 CHAR(24) NULL,
	s_dist_03 CHAR(24) NULL,
	s_dist_04 CHAR(24) NULL,
	s_dist_05 CHAR(24) NULL,
	s_dist_06 CHAR(24) NULL,
	s_dist_07 CHAR(24) NULL,
	s_dist_08 CHAR(24) NULL,
	s_dist_09 CHAR(24) NULL,
	s_dist_10 CHAR(24) NULL,
	s_ytd INT8 NULL,
	s_order_cnt INT8 NULL,
	s_remote_cnt INT8 NULL,
	s_data VARCHAR(50) NULL,
	CONSTRAINT stock_pkey PRIMARY KEY (s_w_id, s_i_id)
);

CREATE TABLE public.order_line
(
	ol_o_id INT8 NOT NULL,
	ol_d_id INT8 NOT NULL,
	ol_w_id INT8 NOT NULL,
	ol_number INT8 NOT NULL,
	ol_i_id INT8 NOT NULL,
	ol_supply_w_id INT8 NULL,
	ol_delivery_d TIMESTAMP NULL,
	ol_quantity INT8 NULL,
	ol_amount DECIMAL(6,2) NULL,
	ol_dist_info CHAR(24) NULL,
	CONSTRAINT order_line_pkey PRIMARY KEY (ol_w_id, ol_d_id, ol_o_id, ol_number)
);
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

