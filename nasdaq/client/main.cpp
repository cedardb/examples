#include <string_view>
#include "NasdaqClient.h"
#include <iostream>

int main(int argc, char *argv[])
{
    if (argc != 3) {
        std::cerr << "usage " << argv[0] << " <bindMountData> <localData>" << std::endl;
        exit(1);
    }

    NasdaqClient client;
    client.connect("localhost", "5432", "client", "client");

    std::string sqlPath = "./";
    std::string bindMountPath = argv[1];
    std::string dataPath = argv[2];

    client.createSchema(sqlPath + "schema.sql");
    client.loadStaticData(bindMountPath + "stocks.csv", bindMountPath + "marketMakers.csv");
    client.loadPremarketData(bindMountPath + "ordersPreMarket.csv", bindMountPath + "executionsPreMarket.csv", bindMountPath + "cancellationsPreMarket.csv");

    client.runExchange(dataPath + "orders.csv", dataPath + "executions.csv", dataPath + "cancellations.csv");

    return 0;
}
