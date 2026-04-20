#include "NasdaqClient.h"
#include <chrono>
#include <iostream>
#include <print>
#include <string_view>
#include <thread>

int main(int argc, char* argv[]) {
   if (argc != 11) {
      std::cerr << "usage " << argv[0]
                << " <DB_HOST> <DB_PORT> <DB_USER> <DB_PASSWORD> "
                   " <DB2_HOST> <DB2_PORT> <DB2_USER> <DB2_PASSWORD> "
                   "<DB_DATA_DIR> <CLIENT_DATA_DIR>"
                << std::endl;
      std::cerr << "<DB_DATA_DIR>: The data location relative to the database "
                   "server. Required to get the path of COPY statements correct."
                << std::endl;
      std::cerr << "<CLIENT_DATA_DIR>: The data location relative to this client."
                << std::endl;
      exit(1);
   }

   NasdaqClient client;
   NasdaqClient client2;
   {
      std::jthread t1([&] { client.connect(argv[1], argv[2], argv[3], argv[4]); });
      std::jthread t2([&] { client2.connect(argv[5], argv[6], argv[7], argv[8]); });
   }

   std::string sqlPath = "./";
   std::string serverDataPath = argv[9];
   std::string dataPath = argv[10];

   {
      std::println("--- CREATING NASDAQ SCHEMA ---");
      std::jthread t1([&] { client.createSchema(sqlPath + "schema.sql"); });
      std::jthread t2([&] { client2.createSchema(sqlPath + "schema.sql"); });
   }

   client2.resetDatabaseStatistics();

   {
      std::println("--- LOADING STATIC NASDAQ DATA ---");
      std::jthread t1([&] {
         client.loadStaticData(serverDataPath + "stocks.csv",
                               serverDataPath + "marketMakers.csv");
      });
      std::jthread t2([&] {
         client2.loadStaticData(serverDataPath + "stocks.csv",
                                serverDataPath + "marketMakers.csv");
      });
   }

   {
      std::println("--- LOADING PREMARKET NASDAQ DATA ---");
      std::jthread t1([&] {
         client.loadPremarketData(serverDataPath + "ordersPreMarket.csv",
                                  serverDataPath + "executionsPreMarket.csv",
                                  serverDataPath + "cancellationsPreMarket.csv");
      });
      std::jthread t2([&] {
         client2.loadPremarketData(serverDataPath + "ordersPreMarket.csv",
                                   serverDataPath + "executionsPreMarket.csv",
                                   serverDataPath + "cancellationsPreMarket.csv");
      });
   }

   {
      std::println("--- RUNNING NASDAQ EXCHANGE ---");
      auto startTime = time_point_cast<std::chrono::nanoseconds>(
                          std::chrono::steady_clock::now())
                          .time_since_epoch()
                          .count();

      std::jthread t1([&] {
         client.runExchange(dataPath + "orders.csv", dataPath + "executions.csv",
                            dataPath + "cancellations.csv", startTime);
      });
      std::jthread t2([&] {
         client2.runExchange(dataPath + "orders.csv", dataPath + "executions.csv",
                             dataPath + "cancellations.csv", startTime);
      });
   }

   return 0;
}
