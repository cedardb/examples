//
// Created by lukie on 15.05.2024.
//

#include "NasdaqClient.h"
#include "thirdparty/csv.h"

#include <cassert>
#include <chrono>
#include <fstream>
#include <iostream>
#include <string_view>
#include <sstream>

static void noticeProcessor(void* /*arg*/, const char* /*message*/) {}

void NasdaqClient::connect(std::string_view host, std::string_view port, std::string_view user, std::string_view password)
{

    std::stringstream connectionString;
    if (!host.empty())
        connectionString << "host=" << host << " ";
    if (!port.empty())
        connectionString << "port=" << port << " ";
    if (!user.empty())
        connectionString << "user=" << user << " ";
    if (!password.empty())
        connectionString << "password=" << password << " ";

    auto str = connectionString.str();
    conn = PQconnectdb(str.c_str());
    if (const auto res = PQstatus(conn); res == CONNECTION_OK) {
        PQsetNoticeProcessor(conn, noticeProcessor, nullptr);
    } else {
        throw std::runtime_error("Could not connect to database");
    }
}

void NasdaqClient::createSchema(std::string_view schemaPath) const
{
    loadFile(std::string(schemaPath));
}

void NasdaqClient::loadStaticData(std::string_view stocksPath, std::string_view marketMakerPath) const
{
    loadCSV("stocks", stocksPath);
    loadCSV("marketmakers", marketMakerPath);
}

void NasdaqClient::loadPremarketData(std::string_view ordersPath, std::string_view executionsPath, std::string_view cancellationsPath) const
{
    loadCSV("orders", ordersPath);
    loadCSV("executions", executionsPath);
    loadCSV("cancellations", cancellationsPath);
}

void NasdaqClient::consume(PGconn* conn, size_t msgCount)
{
    // BEGIN
    auto tuples = PQgetResult(conn);
    if (PQresultStatus(tuples) != PGRES_COMMAND_OK)
        throw std::runtime_error("Unexpected pipeline message");

    auto endOfQuery = PQgetResult(conn);
    if (endOfQuery != nullptr)
        throw std::runtime_error("Unexpected pipeline message");

    for (int i = 0; i < msgCount; ++i)
    {
        tuples = PQgetResult(conn);
        if (PQresultStatus(tuples) != PGRES_COMMAND_OK)
            throw std::runtime_error("Unexpected pipeline message");


        char* val = PQcmdTuples(tuples);
        if(val != std::string_view("1") && val != std::string_view("0"))
            throw std::runtime_error("Unexpected pipeline message");

        endOfQuery = PQgetResult(conn);
        if (endOfQuery != nullptr)
            throw std::runtime_error("Unexpected pipeline message");
    }
    // COMMIT
    tuples = PQgetResult(conn);
    if (PQresultStatus(tuples) != PGRES_COMMAND_OK)
        throw std::runtime_error("Unexpected pipeline message");

    endOfQuery = PQgetResult(conn);
    if (endOfQuery != nullptr)
        throw std::runtime_error("Unexpected pipeline message");
}

void NasdaqClient::sendOrder(const Order &order) const
{

    const char* paramValues[8];
    int paramLengths[8];

    auto stockStr = std::to_string(order.stockId);
    auto tsStr = std::to_string(order.timestamp);
    auto orderStr = std::to_string(order.orderId);
    auto quantStr = std::to_string(order.quantity);
    auto prevStr = std::to_string(order.prevOrder);

    paramValues[0] = stockStr.c_str();
    paramValues[1] = tsStr.c_str();
    paramValues[2] = orderStr.c_str();
    paramValues[3] = order.side.empty()? nullptr : order.side.c_str();
    paramValues[4] = quantStr.c_str();
    paramValues[5] = order.price.c_str();
    paramValues[6] = order.attribution.empty() ? nullptr : order.attribution.c_str();
    paramValues[7] = order.prevOrder == 0 ? nullptr : prevStr.c_str();

    paramLengths[0] = strlen(paramValues[0]);
    paramLengths[1] = strlen(paramValues[1]);
    paramLengths[2] = strlen(paramValues[2]);
    paramLengths[3] = order.side.empty()? 0 : strlen(paramValues[3]);
    paramLengths[4] = strlen(paramValues[4]);
    paramLengths[5] = strlen(paramValues[5]);
    paramLengths[6] = order.attribution.empty() ? 0 : strlen(paramValues[6]);
    paramLengths[7] = order.prevOrder == 0 ? 0 : strlen(paramValues[7]);

    if (PQsendQueryPrepared(conn, "newOrder", 8, paramValues, paramLengths, nullptr, 0) != 1)
        throw std::runtime_error("Could not send new order");
}

void NasdaqClient::sendOrderbookAdd(uint64_t orderId, unsigned stockId, const std::string& side, const std::string& price,
    unsigned quantity) const
{
    const char* paramValues[5];
    int paramLengths[5];


    auto orderStr = std::to_string(orderId);
    auto stockStr = std::to_string(stockId);
    auto quantStr = std::to_string(quantity);

    paramValues[0] = orderStr.c_str();
    paramValues[1] = stockStr.c_str();
    paramValues[2] = side.c_str();
    paramValues[3] = price.empty() ? nullptr : price.c_str();
    paramValues[4] = quantStr.c_str();

    paramLengths[0] = strlen(paramValues[0]);
    paramLengths[1] = strlen(paramValues[1]);
    paramLengths[2] = strlen(paramValues[2]);
    paramLengths[3] = price.empty() ? 0 : strlen(paramValues[3]);
    paramLengths[4] = strlen(paramValues[4]);

    if (PQsendQueryPrepared(conn, "addToOrderbook", 5, paramValues, paramLengths, nullptr, 0) != 1)
        throw std::runtime_error("Could not add to order book");

}

void NasdaqClient::sendOrderbookDelete(uint64_t orderId) const
{
    const char* paramValues[1];
    int paramLengths[1];


    auto orderStr = std::to_string(orderId);

    paramValues[0] = orderStr.c_str();

    paramLengths[0] = strlen(paramValues[0]);

    if (PQsendQueryPrepared(conn, "deleteFromOrderbook", 1, paramValues, paramLengths, nullptr, 0) != 1)
        throw std::runtime_error("could not delete from order book");
}

void NasdaqClient::sendOrderbookReduce(uint64_t orderId, unsigned quantity) const
{
    const char* paramValues[2];
    int paramLengths[2];


    auto orderStr = std::to_string(orderId);
    auto quantStr = std::to_string(quantity);

    paramValues[0] = orderStr.c_str();
    paramValues[1] = quantStr.c_str();
    paramLengths[0] = strlen(paramValues[0]);
    paramLengths[1] = strlen(paramValues[1]);

    if (PQsendQueryPrepared(conn, "reduceInOrderbook", 2, paramValues, paramLengths, nullptr, 0) != 1)
        throw std::runtime_error("could not reduce in order book");
}

void NasdaqClient::sendExecution(const Execution& execution) const
{
    const char* paramValues[5];
    int paramLengths[5];


    auto tsStr = std::to_string(execution.timestamp);
    auto orderStr = std::to_string(execution.orderId);
    auto stockStr = std::to_string(execution.stockId);
    auto quantStr = std::to_string(execution.quantity);

    paramValues[0] = tsStr.c_str();
    paramValues[1] = orderStr.c_str();
    paramValues[2] = stockStr.c_str();
    paramValues[3] = quantStr.c_str();
    paramValues[4] = execution.price.empty() ? nullptr : execution.price.c_str();

    paramLengths[0] = strlen(paramValues[0]);
    paramLengths[1] = strlen(paramValues[1]);
    paramLengths[2] = strlen(paramValues[2]);
    paramLengths[3] = strlen(paramValues[3]);
    paramLengths[4] = execution.price.empty() ? 0 : strlen(paramValues[4]);



    if (PQsendQueryPrepared(conn, "newExecution", 5, paramValues, paramLengths, nullptr, 0) != 1)
        throw std::runtime_error("could not add new execution");
}

void NasdaqClient::sendCancellation(const Cancellation& cancellation) const
{
    const char* paramValues[5];
    int paramLengths[5];


    auto tsStr = std::to_string(cancellation.timestamp);
    auto orderStr = std::to_string(cancellation.orderId);
    auto stockStr = std::to_string(cancellation.stockId);
    auto quantStr = std::to_string(cancellation.quantity);

    paramValues[0] = tsStr.c_str();
    paramValues[1] = orderStr.c_str();
    paramValues[2] = stockStr.c_str();
    paramValues[3] = cancellation.quantity == 0 ? nullptr : quantStr.c_str();

    paramLengths[0] = strlen(paramValues[0]);
    paramLengths[1] = strlen(paramValues[1]);
    paramLengths[2] = strlen(paramValues[2]);
    paramLengths[3] = cancellation.quantity == 0 ? 0 : strlen(paramValues[3]);

    if (PQsendQueryPrepared(conn, "newCancellation", 4, paramValues, paramLengths, nullptr, 0) != 1)
        throw std::runtime_error("could not cancel order");
}

void NasdaqClient::finalize(size_t msgCount) const
{
    if (PQsendQueryPrepared(conn, "commit", 0, nullptr, nullptr, nullptr, 0) != 1)
        throw std::runtime_error("could not commit transaction");

    sendFlushRequest(conn);
    flush(conn);

    consume(conn, msgCount);
}

void NasdaqClient::runExchange(const std::string& ordersPath, const std::string& executionsPath, const std::string& cancellationsPath) const
{
    uint64_t base = 34200000000000; // Market open in nanoseconds since midnight (9:30 AM)
    auto startTime = time_point_cast<std::chrono::nanoseconds>(std::chrono::steady_clock::now()).time_since_epoch().count();

    io::CSVReader<8, io::trim_chars<' '>, io::no_quote_escape<';'>> orderReader(ordersPath);
    io::CSVReader<5, io::trim_chars<' '>, io::no_quote_escape<';'>> executionsReader(executionsPath);
    io::CSVReader<4, io::trim_chars<' '>, io::no_quote_escape<';'>> cancellationsReader(cancellationsPath);

    orderReader.read_header(io::ignore_extra_column, "stockId", "timestamp", "orderId", "side", "quantity", "price", "attribution", "prevOrder");
    executionsReader.read_header(io::ignore_extra_column, "timestamp", "orderId", "stockId", "quantity", "price");
    cancellationsReader.read_header(io::ignore_extra_column, "timestamp", "orderId", "stockId", "quantity");


    prepare(conn, "newOrder", "INSERT INTO orders VALUES($1, $2, $3, $4, $5, $6, $7, $8);");
    prepare(conn, "newExecution", "INSERT INTO executions VALUES($1, $2, $3, $4, $5);");
    prepare(conn, "newCancellation", "INSERT INTO cancellations VALUES($1, $2, $3, $4);");
    prepare(conn, "addToOrderbook", "INSERT INTO orderbook VALUES($1, $2, $3, $4, $5);");
    prepare(conn, "deleteFromOrderbook", "DELETE FROM orderbook WHERE orderId = $1;");
    prepare(conn, "reduceInOrderbook", "UPDATE orderbook SET quantity = quantity - $2 WHERE orderId = $1;");
    prepare(conn, "commit", "COMMIT;");
    prepare(conn, "begin", "BEGIN;");

    enterPipelineMode(conn);


    Order order;
    Execution execution;
    Cancellation cancellation;
    // Populate the first values
    orderReader.read_row(order.stockId, order.timestamp, order.orderId, order.side, order.quantity, order.price, order.attribution, order.prevOrder);
    executionsReader.read_row(execution.timestamp, execution.orderId, execution.stockId, execution.quantity, execution.price);
    cancellationsReader.read_row(cancellation.timestamp, cancellation.orderId, cancellation.stockId, cancellation.quantity);



    while (true)
    {
        auto curTime = time_point_cast<std::chrono::nanoseconds>(std::chrono::steady_clock::now()).time_since_epoch().count();
        auto limit = base + (curTime - startTime);
        size_t counter = 0;

        // Start a transaction
        if (PQsendQueryPrepared(conn, "begin", 0, nullptr, nullptr, nullptr, 0) != 1)
            throw std::runtime_error("could not start transaction");


        // Write orders
        uint64_t prevTimestamp = order.timestamp;
        while (order.timestamp < limit)
        {
            sendOrder(order);
            ++counter;

            // Insert new order into orderbook
            sendOrderbookAdd(order.orderId, order.stockId, order.side, order.price, order.quantity);
            ++counter;

            if (order.prevOrder != 0)
            {
                // Remove the old order from the orderbook
                sendOrderbookDelete(order.prevOrder);
                ++counter;
            }
            orderReader.read_row(order.stockId, order.timestamp, order.orderId, order.side, order.quantity, order.price, order.attribution, order.prevOrder);
            assert(order.timestamp >= prevTimestamp);
            prevTimestamp = order.timestamp;
        }

        // Write executions
        prevTimestamp = execution.timestamp;
        while (execution.timestamp < limit)
        {
            sendExecution(execution);
            ++counter;
            if (execution.orderId != 0) // Only visible orders change the order book
            {
                sendOrderbookReduce(execution.orderId, execution.quantity);
                ++counter;
            }

            executionsReader.read_row(execution.timestamp, execution.orderId, execution.stockId, execution.quantity, execution.price);
            assert(execution.timestamp >= prevTimestamp);
            prevTimestamp = execution.timestamp;

        }


        // Write cancellations

        prevTimestamp = cancellation.timestamp;
        while (cancellation.timestamp < limit)
        {
            sendCancellation(cancellation);
            ++counter;

            if (cancellation.quantity == 0) // Full delete
            {
                sendOrderbookDelete(cancellation.orderId);
            } else // Partial delete
            {
                sendOrderbookReduce(cancellation.orderId, cancellation.quantity);
            }
            ++counter;

            cancellationsReader.read_row(cancellation.timestamp, cancellation.orderId, cancellation.stockId, cancellation.quantity);
            assert(cancellation.timestamp >= prevTimestamp);
            prevTimestamp = cancellation.timestamp;
        }

        std::cout << "Messages: " << counter << std::endl;

        finalize(counter);

        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    exitPipelineMode(conn);
}

void NasdaqClient::close() const noexcept
{
    PQfinish(conn);
}

NasdaqClient::~NasdaqClient()
{
    close();
}

void NasdaqClient::loadFile(const std::string& filePath) const
{
        assert(conn);
        std::ifstream t(filePath);
        if (!t.is_open())
            throw std::runtime_error("could not open file: " + filePath);

        return exec(conn, std::string(std::istreambuf_iterator(t), std::istreambuf_iterator<char>()));
}

void NasdaqClient::exec(PGconn* conn, const std::string& command)
{
    assert(conn);

    PGresult* res = PQexec(conn, command.c_str());
    if (PQresultStatus(res) != PGRES_COMMAND_OK && PQresultStatus(res) != PGRES_TUPLES_OK)
        throw std::runtime_error("could not execute command: " + command);
}

void NasdaqClient::loadCSV(std::string_view tblName, std::string_view path) const
{
    assert(conn);
    std::stringstream stocksCmd;
    stocksCmd << "COPY " << tblName << " FROM '" << path << "' with(format text, delimiter ';', null '', header true);";
    exec(conn, stocksCmd.str());
}

void NasdaqClient::prepare(PGconn* conn, const std::string& name, const std::string& command)
{

    {
        auto res = PQprepare(conn, name.c_str(), command.c_str(), 0, nullptr);
        if (PQresultStatus(res) != PGRES_COMMAND_OK && PQresultStatus(res) != PGRES_TUPLES_OK)
            throw std::runtime_error("could not prepare query: " + command);
    }
}

void NasdaqClient::enterPipelineMode(PGconn* conn)
{
    assert(conn);
    if (PQenterPipelineMode(conn) != 1)
        throw std::runtime_error("could not enter pipeline mode");
}

void NasdaqClient::exitPipelineMode(PGconn* conn)
{
    assert(conn);
    if (PQexitPipelineMode(conn) != 1)
        throw std::runtime_error("could not exit pipeline mode");

}

void NasdaqClient::pipelineSync(PGconn* conn)
// Mark a synchronization point
{
    assert(conn);
    if (PQpipelineSync(conn) != 1)
        throw std::runtime_error("could not sync pipeline");

    auto res = PQgetResult(conn);
    if (PQresultStatus(res) != PGRES_COMMAND_OK && PQresultStatus(res) != PGRES_TUPLES_OK)
        throw std::runtime_error("could not sync pipeline");
    if (PQgetResult(conn) != nullptr)
        throw std::runtime_error("could not sync pipeline");
}

void NasdaqClient::flush(PGconn* conn)
{
    if (PQflush(conn) != 0)
        throw std::runtime_error("could not flush connection");
}

void NasdaqClient::sendFlushRequest(PGconn* conn)
{
    if (PQsendFlushRequest(conn) != 1)
        throw std::runtime_error("could not send connection flush request");
}
