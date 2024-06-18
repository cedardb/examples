# Replaying the NASDAQ order book

This is an example project live-replaying the complete NASDAQ exchange orders from 30-12-2019 with CedarDB.

The example consists of separate applications:

1. A parser written in Python that parses NASDAQ's proprietary ITCHv5 protocol into human-readable CSV files
2. A C++ client connecting to CedarDB and live-replaying all orders.


## Getting started

1. Download the raw binary package capture that NASDAQ provides:
```shell
mkdir data && cd data
wget https://emi.nasdaq.com/ITCH/Nasdaq%20ITCH/12302019.NASDAQ_ITCH50.gz
gunzip 12302019.NASDAQ_ITCH50.gz
```

2. Transform the binary data into CSV files (will take a few minutes)

```shell
cd ../parser
python3 parser.py 
```

You now have a set of files containing the stock exchange events:

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

3. Build the client

```shell
cmake -DCMAKE_BUILD_TYPE=Release
make
```

4. Start cedardb

```shell
./server
```

5. Create a user for the stocks database:

```shell
psql -h /tmp -U postgres -c "create user client superuser; alter user client with password 'client'; create database client;"
```

6. Start the client

```shell
./NasdaqDriver /absolute/path/to/nasdaq/data/
```


You can now connect with a second client and query all tables while they are updated in real-time.

You can alternatively also adapt and use the supplied `run.sh` file to start everything automatically