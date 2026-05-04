#include "NasdaqClient.h"
#include <iostream>
#include <string_view>

int main(int argc, char* argv[]) {
   if (argc != 4) {
      std::cerr << "usage " << argv[0] << " <CLIENT_NASDAQ_DIR> <DB_NASDAQ_DIR> <DB_CONNECTION_STRING>" << std::endl;
      std::cerr << "<CLIENT_NASDAQ_DIR>: The data location relative to this client." << std::endl;
      std::cerr << "<DB_NASDAQ_DIR>: The data location relative to the CedarDB server. Required to get the path of COPY statements correct." << std::endl;
      std::cerr << "<DB_CONNECTION_STRING>: PostgreSQL/libpq connection string with format postgresql://user:password@host:port/database" << std::endl;
      exit(1);
   }

   NasdaqClient client;
   client.connect(argv[3]);

   std::string sqlPath = "./";
   std::string dataPath = argv[1];
   std::string serverDataPath = argv[2];

   client.createSchema(sqlPath + "schema.sql");
   client.loadStaticData(serverDataPath + "stocks.csv", serverDataPath + "marketMakers.csv");
   client.loadPremarketData(serverDataPath + "ordersPreMarket.csv", serverDataPath + "executionsPreMarket.csv", serverDataPath + "cancellationsPreMarket.csv");

   client.runExchange(dataPath + "orders.csv", dataPath + "executions.csv", dataPath + "cancellations.csv");

   return 0;
}
