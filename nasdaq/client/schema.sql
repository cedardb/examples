drop table if exists orderbook;
drop table if exists executions;
drop table if exists cancellations;
drop table if exists orders;
drop table if exists marketMakers;
drop table if exists stocks;

create table stocks
(
    stockId                     int primary key,
    name                        text unique,
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

create table marketmakers
(
    timestamp   bigint,
    stockId     int,
    name        text,
    isPrimary   bool,
    mode        text,
    state       text
);

create table orders
(
    stockId     int not null,
    timestamp   bigint not null,
    orderId     bigint primary key not null,
    side        text,
    quantity    int not null,
    price       numeric(10,4) not null,
    attribution text,
    prevOrder   bigint
);

create table executions
(
    timestamp   bigint not null,
    orderId     bigint,
    stockId     int not null,
    quantity    int not null,
    price       numeric(10,4)
);

create table cancellations
(
    timestamp   bigint not null,
    orderId     bigint not null,
    stockId     int not null,
    quantity    int
);

create table orderbook
(
    orderId     bigint,
    stockId     int,
    side        text,
    price       numeric(10,4),
    quantity    int,
    primary key(orderid, price)
);
commit;

create index on orderbook(orderId);
create index on cancellations(timestamp);
create index on executions(timestamp);
create index on orders(timestamp);
