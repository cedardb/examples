begin;
DROP TABLE IF EXISTS orderbook;
DROP TABLE IF EXISTS executions;
DROP TABLE IF EXISTS cancellations;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS marketMakers;
DROP TABLE IF EXISTS stocks;

CREATE TABLE stocks
(
    stockId                     int primary key,
    name                        text,
    marketCategory              text,
    financialStatusIndicator    text,
    roundLotSize                int,
    roundLotsOnly               bool,
    issueClassification         text,
    issueSubType                text,
    authenticity                text,
    shortSaleThresholdIndicator bool,
    IPOFlag                     bool,
    LULDReferencePriceTier      text,
    ETPFlag                     bool,
    ETPLeverageFactor           int,
    InverseIndicator            bool
);

CREATE TABLE marketmakers
(
    timestamp   bigint,
    stockId     int references stocks,
    name        text,
    isPrimary   bool,
    mode        text,
    state       text
);

CREATE TABLE orders
(
    stockId     int references stocks,
    timestamp   bigint,
    orderId     bigint primary key,
    side        text,
    quantity    int,
    price       numeric(10,4),
    attribution text,
    prevOrder   bigint
);

CREATE TABLE executions
(
    timestamp   bigint,
    orderId     bigint, --references orders,
    stockId     int references stocks,
    quantity    int,
    price       numeric(10,4)
);


CREATE TABLE cancellations
(
    timestamp   bigint,
    orderId     bigint, --references orders,
    stockId     int references stocks,
    quantity    int
);

CREATE TABLE orderbook
(
    orderId     bigint references orders,
    stockId     int references stocks,
    side        text,
    price       numeric(10,4),
    quantity    int,
    primary key(orderid, price)
);
commit;
begin bulk write;
create index on orderbook(orderId);
create index on cancellations(timestamp);
create index on executions(timestamp);
create index on orders(timestamp);
commit;