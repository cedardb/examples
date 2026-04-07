#include "NasdaqClient.h"
#include <iostream>
#include <print>
#include <string_view>

int main(int argc, char *argv[]) {
  if (argc != 7) {
    std::cerr << "usage " << argv[0]
              << " <DB_HOST> <DB_PORT> <DB_USER> <DB_PASSWORD> "
                 "<CEDAR_DATA_DIR> <CLIENT_DATA_DIR>"
              << std::endl;
    std::cerr << "<CEDAR_DATA_DIR>: The data location relative to the CedarDB "
                 "server. Required to get the path of COPY statements correct."
              << std::endl;
    std::cerr << "<CLIENT_DATA_DIR>: The data location relative to this client."
              << std::endl;
    exit(1);
  }

  NasdaqClient client;
  client.connect(argv[1], argv[2], argv[3], argv[4]);

  std::string sqlPath = "./";
  std::string serverDataPath = argv[5];
  std::string dataPath = argv[6];

  std::println("--- CREATING NASDAQ SCHEMA ---");
  client.createSchema(sqlPath + "schema.sql");
  std::println("--- LOADING STATIC NASDAQ DATA ---");
  client.loadStaticData(serverDataPath + "stocks.csv",
                        serverDataPath + "marketMakers.csv");
  std::println("--- LOADING PREMARKET NASDAQ DATA ---");
  client.loadPremarketData(serverDataPath + "ordersPreMarket.csv",
                           serverDataPath + "executionsPreMarket.csv",
                           serverDataPath + "cancellationsPreMarket.csv");

  std::println("--- RUNNING NASDAQ EXCHANGE ---");
  client.runExchange(dataPath + "orders.csv", dataPath + "executions.csv",
                     dataPath + "cancellations.csv");

  return 0;
}
