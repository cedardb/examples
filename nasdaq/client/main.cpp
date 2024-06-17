#include <string_view>
#include "NasdaqClient.h"

int main()
{
    NasdaqClient client;
    client.connect("localhost", "5432", "lukas", "lukas", 1);

    std::string sqlPath = "../../";
    std::string dataPath = "../../data/";

    client.createSchema(sqlPath + "schema.sql");
    client.loadStaticData(dataPath + "stocks.csv", dataPath + "marketMakers.csv");
    client.loadPremarketData(dataPath + "ordersPreMarket.csv", dataPath + "executionsPreMarket.csv", dataPath + "cancellationsPreMarket.csv");

    client.runExchange(dataPath + "orders.csv", dataPath + "executions.csv", dataPath + "cancellations.csv");

    return 0;
}
