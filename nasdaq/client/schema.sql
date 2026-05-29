BEGIN;
DROP TABLE IF EXISTS cancellations;
CREATE TABLE
    cancellations (
        timestamp bigint NOT NULL,
        orderId bigint NOT NULL,
        stockId int NOT NULL,
        quantity int
    );
COMMIT;

BEGIN;
DROP TABLE IF EXISTS executions;
CREATE TABLE
    executions (
        timestamp bigint NOT NULL,
        orderId bigint,
        stockId int NOT NULL,
        quantity int NOT NULL,
        price numeric(10, 4)
    );
COMMIT;

BEGIN;
DROP TABLE IF EXISTS marketMakers;
CREATE TABLE
    marketmakers (
        timestamp bigint,
        stockId int,
        name text,
        isPrimary bool,
        MODE text,
        state text
    );
COMMIT;

BEGIN;
DROP TABLE IF EXISTS orderbook;
CREATE TABLE
    orderbook (
        orderId bigint,
        stockId int,
        side text,
        price numeric(10, 4),
        quantity int,
        PRIMARY KEY (orderid, price)
    );
COMMIT;

BEGIN;
DROP TABLE IF EXISTS orders;
CREATE TABLE
    orders (
        stockId int NOT NULL,
        timestamp bigint NOT NULL,
        orderId bigint PRIMARY KEY NOT NULL,
        side text,
        quantity int NOT NULL,
        price numeric(10, 4) NOT NULL,
        attribution text,
        prevOrder bigint
    );
COMMIT;

BEGIN;
DROP TABLE IF EXISTS stocks;
CREATE TABLE
    stocks (
        stockId int PRIMARY KEY,
        name text UNIQUE,
        marketCategory text,
        financialStatusIndicator text,
        roundLotSize int,
        roundLotsOnly bool,
        issueClassification text,
        issueSubType text,
        authenticity text,
        shortSaleThresholdIndicator bool,
        IPOFlag bool,
        LULDReferencePriceTier text,
        ETPFlag bool,
        ETPLeverageFactor int,
        InverseIndicator bool
    );
COMMIT;

CREATE INDEX ON cancellations (timestamp);
CREATE INDEX ON executions (timestamp);
CREATE INDEX ON orderbook (orderId);
CREATE INDEX ON orders (timestamp);
