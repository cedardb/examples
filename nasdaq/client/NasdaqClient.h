#ifndef NASDAQCLIENT_H
#define NASDAQCLIENT_H

#include <string_view>
#include <bits/stdint-uintn.h>

#include "libpq-fe.h"
#include "thirdparty/csv.h"

class NasdaqClient {
    PGconn* conn;

public:
    void connect(std::string_view host, std::string_view port, std::string_view user, std::string_view password);

    void createSchema(std::string_view schemaPath) const;

    void loadStaticData(std::string_view stocksPath, std::string_view marketMakerPath) const;

    void loadPremarketData(std::string_view ordersPath, std::string_view executionsPath, std::string_view cancellationsPath) const;

    void runExchange(const std::string& ordersPath, const std::string& executionsPath, const std::string& cancellationsPath) const;

    void close() const noexcept;

    virtual ~NasdaqClient();

private:

    struct Order
    {
        unsigned stockId;
        uint64_t timestamp;
        uint64_t orderId;
        std::string side;
        unsigned quantity;
        std::string price;
        std::string attribution;
        uint64_t prevOrder;
    };


    struct Execution
    {
        uint64_t timestamp;
        uint64_t orderId;
        unsigned stockId;
        unsigned quantity;
        std::string price;
    };

    struct Cancellation
    {
        uint64_t timestamp;
        uint64_t orderId;
        unsigned stockId;
        unsigned quantity;
    };

    void loadFile(const std::string& filePath) const;

    static void exec(PGconn* conn, const std::string& command);

    void loadCSV(std::string_view tblName, std::string_view path) const;

    static void prepare(PGconn* conn, const std::string& name, const std::string& command);

    static void enterPipelineMode(PGconn* conn);

    static void exitPipelineMode(PGconn* conn);

    static void flush(PGconn* conn);

    static void sendFlushRequest(PGconn* conn);

    static void pipelineSync(PGconn* conn);

    static void consume(PGconn* conn, size_t msgCount);

    void sendOrder(const Order& order) const;

    void sendOrderbookAdd(uint64_t orderId, unsigned stockId, const std::string& side, const std::string& price, unsigned quantity) const;

    void sendOrderbookDelete(uint64_t orderId) const;

    void sendOrderbookReduce(uint64_t orderId, unsigned quantity) const;

    void sendExecution(const Execution& execution) const;

    void sendCancellation(const Cancellation& cancellation) const;

    void finalize(size_t msgCount) const;
};

#endif //NASDAQCLIENT_H
