import csv
from dataclasses import dataclass

import struct

ORDER_ADD_ID = b'A'
ORDER_ADD_WITH_MPID_ID = b'F'
ORDER_EXECUTE_ID = b'E'
ORDER_EXECUTE_WITH_PRICE_ID = b'C'
ORDER_CANCEL_ID = b'X'
ORDER_DELETE_ID = b'D'
ORDER_REPLACE_ID = b'U'
TRADE_ID = b'P'
STOCK_DIRECTORY_ID = b'R'
MARKET_MAKER_ID = b'L'

MARKET_OPEN_TS = 34200000000000
orderSchema = [
    "stockId",
    "timestamp",
    "orderId",
    "side",
    "quantity",
    "price",
    "attribution",
    "prevOrder"
]

executionSchema = [
    "timestamp",
    "orderId",
    "stockId",
    "quantity",
    "price"
]

cancellationSchema = [
    "timestamp",
    "orderId",
    "stockId",
    "quantity"
]

stocksSchema = [
    "stockId",
    "name",
    "marketCategory",
    "financialStatusIndicator",
    "roundLotSize",
    "roundLotsOnly",
    "issueClassification",
    "issueSubType",
    "authenticity",
    "shortSaleThresholdIndicator",
    "IPOFlag",
    "LULDReferencePriceTier",
    "ETPFlag",
    "ETPLeverageFactor",
    "InverseIndicator"
]

marketMakerSchema = [
    "timestamp",
    "stockId",
    "name",
    "isPrimary",
    "mode",
    "state"
]


@dataclass
class Order:
    stockId: int
    timestamp: int
    orderId: int
    side: str
    quantity: int
    price: int
    attribution: str
    prevOrder: int


@dataclass
class Execution:
    timestamp: int
    orderId: int
    stockId: int
    quantity: int
    price: int


@dataclass
class Cancellation:
    timestamp: int
    orderId: int
    stockId: int
    quantity: int


@dataclass
class StockDirectoryEntry:
    stockId: int
    name: str
    marketCategory: str
    financialStatusIndicator: str
    roundLotSize: int
    roundLotsOnly: bool
    issueClassification: str
    issueSubType: str
    authenticity: str
    shortSaleThresholdIndicator: bool
    IPOFlag: bool
    LULDReferencePriceTier: str
    ETPFlag: bool
    ETPLeverageFactor: int
    InverseIndicator: bool


@dataclass
class MarketMaker:
    timestamp: int
    stockId: int
    name: str
    primary: bool
    mode: str
    state: str


def sideToStr(side):
    if side == b'B': return "BUY"
    if side == b'S':
        return "SELL"
    else:
        raise ValueError


def handleMarketMakers(pkg):
    (stockId,) = struct.unpack("!H", pkg[1:3])
    (timestamp,) = struct.unpack("!6s", pkg[5:11])
    (name,) = struct.unpack("!4s", pkg[11:15])
    (isPrimary,) = struct.unpack("!c", pkg[23:24])
    (mode,) = struct.unpack("!c", pkg[24:25])
    (state,) = struct.unpack("!c", pkg[25:26])

    timestamp = int.from_bytes(timestamp)
    name = name.decode('ascii').strip()
    isPrimary = True if isPrimary == 'Y' else False
    mode = mode.decode('ascii').strip()
    state = state.decode('ascii').strip()

    return MarketMaker(timestamp, stockId, name, isPrimary, mode, state)


def handleStockDirectory(pkg):
    (stockId,) = struct.unpack("!H", pkg[1:3])
    (name,) = struct.unpack("!8s", pkg[11:19])
    (marketCategory,) = struct.unpack("!c", pkg[19:20])
    (financialStatusIndicator,) = struct.unpack("!c", pkg[20:21])
    (roundLotSize,) = struct.unpack("!I", pkg[21:25])
    (roundLotsOnly,) = struct.unpack("!c", pkg[25:26])
    (issueClassification,) = struct.unpack("!c", pkg[26:27])
    (issueSubType,) = struct.unpack("!2s", pkg[27:29])
    (authenticity,) = struct.unpack("!c", pkg[29:30])
    (shortSaleThresholdIndicator,) = struct.unpack("!c", pkg[30:31])
    (IPOFlag,) = struct.unpack("!c", pkg[31:32])
    (LULDReferencePriceTier,) = struct.unpack("!c", pkg[32:33])
    (ETPFlag,) = struct.unpack("!c", pkg[33:34])
    (ETPLeverageFactor,) = struct.unpack("!I", pkg[34:38])
    (InverseIndicator,) = struct.unpack("!c", pkg[38:39])

    name = name.decode('ascii').strip()
    marketCategory = marketCategory.decode('ascii').strip()
    financialStatusIndicator = financialStatusIndicator.decode('ascii').strip()
    issueClassification = issueClassification.decode('ascii').strip()
    authenticity = authenticity.decode('ascii').strip()
    LULDReferencePriceTier = LULDReferencePriceTier.decode('ascii').strip()

    issueSubType = issueSubType.decode('ascii').strip()
    roundLotsOnly = True if roundLotsOnly == 'Y' else False
    shortSaleThresholdIndicator = True if shortSaleThresholdIndicator == 'Y' else False
    IPOFlag = True if IPOFlag == 'Y' else False
    ETPFlag = True if ETPFlag == 'Y' else False
    InverseIndicator = True if InverseIndicator == 'Y' else False

    return StockDirectoryEntry(stockId, name, marketCategory, financialStatusIndicator, roundLotSize, roundLotsOnly,
                               issueClassification, issueSubType, authenticity, shortSaleThresholdIndicator, IPOFlag,
                               LULDReferencePriceTier, ETPFlag, ETPLeverageFactor, InverseIndicator)


def handleOrderAdd(pkg):
    (stockId,) = struct.unpack("!H", pkg[1:3])
    (timestamp,) = struct.unpack("!6s", pkg[5:11])
    (orderId,) = struct.unpack("!Q", pkg[11:19])
    (side,) = struct.unpack("!c", pkg[19:20])
    (quantity,) = struct.unpack("!I", pkg[20:24])
    (price,) = struct.unpack("!I", pkg[32:36])

    timestamp = int.from_bytes(timestamp)
    side = sideToStr(side)

    return Order(stockId, timestamp, orderId, side, quantity, price / 10000, None, None)


def handleOrderAddWithAttribution(pkg):
    (stockId,) = struct.unpack("!H", pkg[1:3])
    (timestamp,) = struct.unpack("!6s", pkg[5:11])
    (orderId,) = struct.unpack("!Q", pkg[11:19])
    (side,) = struct.unpack("!c", pkg[19:20])
    (quantity,) = struct.unpack("!I", pkg[20:24])
    (price,) = struct.unpack("!I", pkg[32:36])
    (attribution,) = struct.unpack("!4s", pkg[36:40])

    timestamp = int.from_bytes(timestamp)
    side = sideToStr(side)
    attribution = attribution.decode('ascii').strip()

    return Order(stockId, timestamp, orderId, side, quantity, price / 10000, attribution, None)


def handleOrderExecute(pkg):
    (stockId,) = struct.unpack("!H", pkg[1:3])
    (timestamp,) = struct.unpack("!6s", pkg[5:11])
    (orderId,) = struct.unpack("!Q", pkg[11:19])
    (quantity,) = struct.unpack("!I", pkg[19:23])

    timestamp = int.from_bytes(timestamp)

    return Execution(timestamp, orderId, stockId, quantity, None)


def handleOrderExecuteWithPrice(pkg):
    (stockId,) = struct.unpack("!H", pkg[1:3])
    (timestamp,) = struct.unpack("!6s", pkg[5:11])
    (orderId,) = struct.unpack("!Q", pkg[11:19])
    (quantity,) = struct.unpack("!I", pkg[19:23])
    (price,) = struct.unpack("!I", pkg[32:36])

    timestamp = int.from_bytes(timestamp)

    return Execution(timestamp, orderId, stockId, quantity, price / 10000)


def handleTrade(pkg):
    (stockId,) = struct.unpack("!H", pkg[1:3])
    (timestamp,) = struct.unpack("!6s", pkg[5:11])
    (quantity,) = struct.unpack("!I", pkg[20:24])
    (price,) = struct.unpack("!I", pkg[32:36])

    timestamp = int.from_bytes(timestamp)

    return Execution(timestamp, None, stockId, quantity, price / 10000)


def handleOrderCancel(pkg):
    (stockId,) = struct.unpack("!H", pkg[1:3])
    (timestamp,) = struct.unpack("!6s", pkg[5:11])
    (orderId,) = struct.unpack("!Q", pkg[11:19])
    (quantity,) = struct.unpack("!I", pkg[19:23])

    timestamp = int.from_bytes(timestamp)

    return Cancellation(timestamp, orderId, stockId, quantity)


def handleOrderDelete(pkg):
    (stockId,) = struct.unpack("!H", pkg[1:3])
    (timestamp,) = struct.unpack("!6s", pkg[5:11])
    (orderId,) = struct.unpack("!Q", pkg[11:19])

    timestamp = int.from_bytes(timestamp)

    return Cancellation(timestamp, orderId, stockId, None)


def handleOrderReplace(pkg):
    (stockId,) = struct.unpack("!H", pkg[1:3])
    (timestamp,) = struct.unpack("!6s", pkg[5:11])
    (orderId,) = struct.unpack("!Q", pkg[11:19])
    (newOrderId,) = struct.unpack("!Q", pkg[19:27])
    (quantity,) = struct.unpack("!I", pkg[27:31])
    (price,) = struct.unpack("!I", pkg[31:35])

    timestamp = int.from_bytes(timestamp)

    return Order(stockId, timestamp, newOrderId, None, quantity, price / 10000, None, orderId)


if __name__ == '__main__':
    with (open("../data/orders.csv", "w") as orderFile,
          open("../data/ordersPreMarket.csv", "w") as orderPremarketFile,
          open("../data/executions.csv", "w") as executionFile,
          open("../data/executionsPreMarket.csv", "w") as executionPremarketFile,
          open("../data/cancellations.csv", "w") as cancellationFile,
          open("../data/cancellationsPreMarket.csv", "w") as cancellationPremarketFile,
          open("../data/stocks.csv", "w") as stocksFile,
          open("../data/marketMakers.csv", "w") as marketMakerFile):

        orderWriter = csv.writer(orderFile, delimiter=";")
        orderWriter.writerow(orderSchema)
        orderPremarketWriter = csv.writer(orderPremarketFile, delimiter=";")
        orderPremarketWriter.writerow(orderSchema)

        executionWriter = csv.writer(executionFile, delimiter=";")
        executionWriter.writerow(executionSchema)
        executionPremarketWriter = csv.writer(executionPremarketFile, delimiter=";")
        executionPremarketWriter.writerow(executionSchema)

        cancellationWriter = csv.writer(cancellationFile, delimiter=";")
        cancellationWriter.writerow(cancellationSchema)
        cancellationPremarketWriter = csv.writer(cancellationPremarketFile, delimiter=";")
        cancellationPremarketWriter.writerow(cancellationSchema)

        stocksWriter = csv.writer(stocksFile, delimiter=";")
        stocksWriter.writerow(stocksSchema)

        marketMakerWriter = csv.writer(marketMakerFile, delimiter=";")
        marketMakerWriter.writerow(marketMakerSchema)

        with open("../data/12302019.NASDAQ_ITCH50", mode='rb') as file:
            fileContent = file.read()
            offset = 0
            while offset < len(fileContent):
                (msgLen,) = struct.unpack('!H', fileContent[offset:offset + 2])
                (msgType,) = struct.unpack('!c', fileContent[offset + 2:offset + 3])

                pkg = fileContent[offset + 2:offset + msgLen + 2]
                order = None
                cancellation = None
                execution = None

                if msgType == STOCK_DIRECTORY_ID:
                    directoryEntry = handleStockDirectory(pkg)
                    stocksWriter.writerow(directoryEntry.__dict__.values())

                elif msgType == MARKET_MAKER_ID:
                    marketMaker = handleMarketMakers(pkg)
                    marketMakerWriter.writerow(marketMaker.__dict__.values())

                elif msgType == ORDER_ADD_ID:
                    order = handleOrderAdd(pkg)
                elif msgType == ORDER_ADD_WITH_MPID_ID:
                    order = handleOrderAddWithAttribution(pkg)
                elif msgType == ORDER_REPLACE_ID:
                    order = handleOrderReplace(pkg)

                elif msgType == ORDER_CANCEL_ID:
                    cancellation = handleOrderCancel(pkg)
                elif msgType == ORDER_DELETE_ID:
                    cancellation = handleOrderDelete(pkg)

                elif msgType == ORDER_EXECUTE_ID:
                    execution = handleOrderExecute(pkg)
                elif msgType == ORDER_EXECUTE_WITH_PRICE_ID:
                    execution = handleOrderExecuteWithPrice(pkg)
                elif msgType == TRADE_ID:
                    execution = handleTrade(pkg)

                if order:
                    if order.timestamp < MARKET_OPEN_TS:
                        orderPremarketWriter.writerow(order.__dict__.values())
                    else:
                        orderWriter.writerow(order.__dict__.values())

                if execution:
                    if execution.timestamp < MARKET_OPEN_TS:
                        executionPremarketWriter.writerow(execution.__dict__.values())
                    else:
                        executionWriter.writerow(execution.__dict__.values())

                if cancellation:
                    if cancellation.timestamp < MARKET_OPEN_TS:
                        cancellationPremarketWriter.writerow(cancellation.__dict__.values())
                    else:
                        cancellationWriter.writerow(cancellation.__dict__.values())

                offset += msgLen + 2
