#include "NasdaqClient.h"
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

   // Create the schema before loading any data.
   {
      std::jthread schemaThread([&] { client.createSchema(sqlPath + "schema.sql"); });
      std::jthread schemaThread2([&] {
         if (client2)
            client2->createSchema(sqlPath + "schema.sql");
      });
   }
   // Load static reference tables next.
   {
      std::jthread staticDataThread([&] { client.loadStaticData(serverDataPath + "stocks.csv", serverDataPath + "marketMakers.csv"); });
      std::jthread staticDataThread2([&] {
         if (client2)
            client2->loadStaticData(serverDataPath + "stocks.csv", serverDataPath + "marketMakers.csv");
      });
   }
   // Load premarket events before the main replay.
   {
      std::jthread premarketThread([&] {
         client.loadPremarketData(
            serverDataPath + "ordersPreMarket.csv",
            serverDataPath + "executionsPreMarket.csv",
            serverDataPath + "cancellationsPreMarket.csv");
      });
      std::jthread premarketThread2([&] {
         if (client2) {
            client2->loadPremarketData(
               serverDataPath + "ordersPreMarket.csv",
               serverDataPath + "executionsPreMarket.csv",
               serverDataPath + "cancellationsPreMarket.csv");
         }
      });
   }
   // Run the main exchange workload last.
   {
      std::jthread exchangeThread([&] {
         client.runExchange(dataPath + "orders.csv", dataPath + "executions.csv", dataPath + "cancellations.csv");
      });
      std::jthread exchangeThread2([&] {
         if (client2)
            client2->runExchange(dataPath + "orders.csv", dataPath + "executions.csv", dataPath + "cancellations.csv");
      });
   }

   return 0;
}
