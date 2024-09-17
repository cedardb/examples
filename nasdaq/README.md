# Replaying the NASDAQ order book

This is an example project live-replaying the complete NASDAQ exchange orders from 30-12-2019 with CedarDB.

The example consists of separate applications:

1. A parser written in Python that parses NASDAQ's proprietary ITCHv5 protocol into human-readable CSV files
2. A C++ client connecting to CedarDB and live-replaying all orders.


## Getting started

1. Download the raw binary package capture that NASDAQ provides.
This downloads about 3.3 GB, which decompresses to around 7.7 GB.

```shell
mkdir data && cd data
wget https://emi.nasdaq.com/ITCH/Nasdaq%20ITCH/12302019.NASDAQ_ITCH50.gz
gunzip 12302019.NASDAQ_ITCH50.gz
```

2. Transform the binary data into CSV files using the parser python script (will take a few minutes to write ~10 GB CSV files).

```shell
cd ../parser
python3 parser.py 
```

You should now have a set of files in the data directory containing the stock exchange events:

```shell
cd ..
du -h data/*.csv
```

```
3,4G	cancellations.csv
74M	    cancellationsPreMarket.csv
230M	executions.csv
1,2M	executionsPreMarket.csv
7,4M	marketMakers.csv
6,2G	orders.csv
124M	ordersPreMarket.csv
516K	stocks.csv
```

3. Build the client.
This is a C++ client that connects to CedarDB, or any PostgreSQL compatible database using libpq.
Dependencies on Ubuntu: `g++ cmake libpq-dev`.

```shell
cmake client -DCMAKE_BUILD_TYPE=Release -B bin
cmake --build bin
```

4. Start cedardb

```shell
docker run --rm -p 5432:5432 -v .:/nasdaq --name=cedardb_nasdaq cedardb
```

5. Create a user for the stocks database:

```shell
docker exec -it cedardb_nasdaq psql -h /tmp -U postgres -c "create user client superuser; alter user client with password 'client'; create database client;"
```

6. Start the client.
The client connects to CedarDB, loads the static `schema.sql`, and loads the static stocks and market makers data.
Then, we load the pre-market orders, executions and cancellations data.

```shell
./bin/NasdaqDriver /nasdaq/data/ data/
```

While the client is running, it replays the live exchange data in 100ms batches, treating the point in time the program was started as 9:30 AM, i.e. the exact instance the market opens.
In the first minute, the client catches up to the live transaction stream and starts inserting many messages.
Afterward, you should get message batches of a couple of thousand messages per 100ms.
So, if you run the client for 30 minutes, the database state will represent the state of the NASDAQ exchange 30 minutes after market open, i.e., 10:00 AM.

For your convenience, you can also use the supplied `run.sh` script to start CedarDB and the Nasdaq driver automatically.

7. Query the data.

You can now connect with another PostgreSQL client and query all tables while they are updated in real-time.
For example, connect with a command-line client, with the default password `client` as configured above.
```shell
psql -h localhost -U client
```

Here are some example queries to get you started:

```sql
client=#
select count(*) from orders;
  count   
----------
 11019259
(1 row)

Time: 5.316 ms
```

```sql
client=#
select avg(price) from executions;
             avg             
-----------------------------
 140.21785151844912886904428
(1 row)

Time: 15.681 ms
```

The following query calculates the new orders created per second averaged over the last 10 seconds.

```sql
client=#
select count(*) / 10 as new -- averaged over 10 seconds
from  orders o
where prevOrder is null -- == new order
and o.timestamp > (select max(e.timestamp) from executions e) - 10::bigint * 1000 * 1000 * 1000; -- averaged over 10 seconds
 new  
------
 8285
(1 row)

Time: 32.514 ms
```

You can find some more complex queries in the `sql` subdirectory.

## Load everything

To load the complete dataset all at once, you can start CedarDB with the steps until 5., and then directly copy the CSV data:

```shell
psql -h localhost -U client
```

```sql
\i schema.sql
\copy stocks from data/stocks.csv with(format text, delimiter ';', null '', header true)
\copy marketmakers from data/marketMakers.csv with(format text, delimiter ';', null '', header true)
\copy orders from data/ordersPreMarket.csv with(format text, delimiter ';', null '', header true)
\copy orders from data/orders.csv with(format text, delimiter ';', null '', header true)
\copy executions from data/executionsPreMarket.csv with(format text, delimiter ';', null '', header true)
\copy executions from data/executions.csv with(format text, delimiter ';', null '', header true)
\copy cancellations from data/cancellationsPreMarket.csv with(format text, delimiter ';', null '', header true)
\copy cancellations from data/cancellations.csv with(format text, delimiter ';', null '', header true)
```

Please note that this does not maintain the orderbook, which would be maintained by the client.

