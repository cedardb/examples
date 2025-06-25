# Demo showing the use of CockroachDB changefeeds (CDC) with CedarDB

**Offload the analytical query workload to CedarDB!**

Since this demo is meant to illustrate the use of CockroachDB changefeeds, it uses the
"MovR" demo app built into CockroachDB.  The entity relationship diagram (ERD) is shown
below.  Here are a couple of references on these changefeeds and the MovR demo:

- [MovR](https://www.cockroachlabs.com/docs/stable/movr)
- [Cockroach Workload](https://www.cockroachlabs.com/docs/stable/cockroach-workload#movr-workload)
- [CDC into a webhook sink](https://www.cockroachlabs.com/docs/stable/changefeed-examples#create-a-changefeed-connected-to-a-webhook-sink)
- [GitHub repo](https://github.com/cockroachlabs/cdc-webhook-sink-test-server) for Go webhook sink which
  was the inspiration for the code in this repo

![Movr app schema](./movr_schema.png)

## CockroachDB

Initialize the MovR workload, per the docs referenced above:

```bash
cockroach workload init movr "postgresql://root@localhost:26257?sslcert=$HOME/certs/client.root.crt&sslkey=$HOME/certs/client.root.key&sslmode=verify-full&sslrootcert=$HOME/certs/ca.crt"
```

Or, in insecure mode, which is simpler for demos:
```bash
cockroach workload init movr "postgresql://root@localhost:15432/movr?sslmode=disable"
```

## CedarDB

Once that schema exists, we can see what it looks like and replicate it in CedarDB.  Note the absence
of the foreign key constraints here as the intent is to use CedarDB not as the system of record but
as the analytical query engine.

```sql
CREATE TABLE public.users
(
	id UUID NOT NULL,
	city VARCHAR NOT NULL,
	name VARCHAR NULL,
	address VARCHAR NULL,
	credit_card VARCHAR NULL,
	PRIMARY KEY (city, id)
);

CREATE TABLE public.vehicles
(
	id UUID NOT NULL,
	city VARCHAR NOT NULL,
	type VARCHAR NULL,
	owner_id UUID NULL,
	creation_time TIMESTAMP NULL,
	status VARCHAR NULL,
	current_location VARCHAR NULL,
	ext JSONB NULL,
	PRIMARY KEY (city, id)
);

CREATE TABLE public.rides
(
	id UUID NOT NULL,
	city VARCHAR NOT NULL,
	vehicle_city VARCHAR NULL,
	rider_id UUID NULL,
	vehicle_id UUID NULL,
	start_address VARCHAR NULL,
	end_address VARCHAR NULL,
	start_time TIMESTAMP NULL,
	end_time TIMESTAMP NULL,
	revenue DECIMAL(10,2) NULL,
	PRIMARY KEY (city, id)
);

CREATE TABLE public.vehicle_location_histories
(
	city VARCHAR NOT NULL,
	ride_id UUID NOT NULL,
	"timestamp" TIMESTAMP NOT NULL,
	lat FLOAT8 NULL,
	long FLOAT8 NULL,
	PRIMARY KEY (city, ride_id, "timestamp")
);

CREATE TABLE public.promo_codes
(
	code VARCHAR NOT NULL,
	description VARCHAR NULL,
	creation_time TIMESTAMP NULL,
	expiration_time TIMESTAMP NULL,
	rules JSONB NULL,
	PRIMARY KEY (code)
);

CREATE TABLE public.user_promo_codes
(
	city VARCHAR NOT NULL,
	user_id UUID NOT NULL,
	code VARCHAR NOT NULL,
	"timestamp" TIMESTAMP NULL,
	usage_count INT8 NULL,
	PRIMARY KEY (city, user_id, code)
);
```

## Start the CDC webhook endpoint

For the time being, [startup.sh](./startup.sh) **will need to be edited** to align to your
environment.  This will all be put into Docker Compose at some point to simplify things.

```bash
./startup.sh
```

## CockroachDB

This is the syntax for creating the changefeeds.  Note that, in the URLs here, there is
an ordered list of the primary key components for each table, where the elements are
separated by comma (`,`).

```sql
CREATE CHANGEFEED FOR TABLE public.users
INTO 'webhook-https://localhost:8443/cdc/city,id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.vehicles
INTO 'webhook-https://localhost:8443/cdc/city,id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.rides
INTO 'webhook-https://localhost:8443/cdc/city,id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.vehicle_location_histories
INTO 'webhook-https://localhost:8443/cdc/city,ride_id,timestamp?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.promo_codes
INTO 'webhook-https://localhost:8443/cdc/code?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.user_promo_codes
INTO 'webhook-https://localhost:8443/cdc/city,user_id,code?insecure_tls_skip_verify=true'
WITH updated;
```

## Start the Movr app

```bash
cockroach workload run movr "postgresql://root@localhost:26257?sslcert=$HOME/certs/client.root.crt&sslkey=$HOME/certs/client.root.key&sslmode=verify-full&sslrootcert=$HOME/certs/ca.crt"
```

## With this app running, make some observations

- We should be able to see the `SELECT COUNT(*) FROM table_name;` increasing in CedarDB for
  each of the MovR tables.
- **TODO:** Based on the ERD, come up with a few nice analytical queries to run.


