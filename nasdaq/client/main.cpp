#include "NasdaqClient.h"
#include <chrono>
#include <iostream>
#include <memory>
#include <string_view>
#include <thread>

int main(int argc, char* argv[]) {
   if (argc != 4 && argc != 5) {
      std::cerr << "usage " << argv[0] << " <CLIENT_NASDAQ_DIR> <DB_NASDAQ_DIR> <DB1_CONNECTION_STRING> [DB2_CONNECTION_STRING]" << std::endl;
      std::cerr << "<CLIENT_NASDAQ_DIR>: The data location relative to this client." << std::endl;
      std::cerr << "<DB_NASDAQ_DIR>: The data location relative to the CedarDB server. Required to get the path of COPY statements correct." << std::endl;
      std::cerr << "<DB#_CONNECTION_STRING>: PostgreSQL/libpq connection string with format postgresql://user:password@host:port/database" << std::endl;
      exit(1);
   }

   NasdaqClient client;
   client.connect(argv[3]);

   std::unique_ptr<NasdaqClient> client2;
   if (argc == 5 && argv[4][0] != '\0') {
      client2 = std::make_unique<NasdaqClient>();
      client2->connect(argv[4]);
   }

   std::string sqlPath = "./";
   std::string dataPath = argv[1];
   std::string serverDataPath = argv[2];

   // 1. Create the schema before loading any data.
   // 2. Load static reference tables next.
   // 3. Load premarket events before the main replay.
   {
      std::jthread schemaThread([&] {
         client.createSchema(sqlPath + "schema.sql");
         client.loadStaticData(serverDataPath + "stocks.csv", serverDataPath + "marketMakers.csv");
         client.loadPremarketData(
            serverDataPath + "ordersPreMarket.csv",
            serverDataPath + "executionsPreMarket.csv",
            serverDataPath + "cancellationsPreMarket.csv");
      });
      std::jthread schemaThread2([&] {
         if (client2) {
            client2->createSchema(sqlPath + "schema.sql");
            client2->loadStaticData(serverDataPath + "stocks.csv", serverDataPath + "marketMakers.csv");
            client2->loadPremarketData(
               serverDataPath + "ordersPreMarket.csv",
               serverDataPath + "executionsPreMarket.csv",
               serverDataPath + "cancellationsPreMarket.csv");
         }
      });
   }

   // 4. Run the main exchange workload last.
   {
      const auto startTime = std::chrono::time_point_cast<std::chrono::nanoseconds>(std::chrono::steady_clock::now()).time_since_epoch().count();
      std::jthread exchangeThread([&] {
         client.runExchange(dataPath + "orders.csv", dataPath + "executions.csv", dataPath + "cancellations.csv", startTime);
      });
      std::jthread exchangeThread2([&] {
         if (client2)
            client2->runExchange(dataPath + "orders.csv", dataPath + "executions.csv", dataPath + "cancellations.csv", startTime);
      });
   }

   return 0;
}
