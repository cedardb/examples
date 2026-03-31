#include <algorithm>
#include <array>
#include <cstdint>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <optional>
#include <sstream>
#include <stdexcept>
#include <string>
#include <unordered_set>
#include <vector>

namespace {

constexpr char ORDER_ADD_ID = 'A';
constexpr char ORDER_ADD_WITH_MPID_ID = 'F';
constexpr char ORDER_EXECUTE_ID = 'E';
constexpr char ORDER_EXECUTE_WITH_PRICE_ID = 'C';
constexpr char ORDER_CANCEL_ID = 'X';
constexpr char ORDER_DELETE_ID = 'D';
constexpr char ORDER_REPLACE_ID = 'U';
constexpr char TRADE_ID = 'P';
constexpr char STOCK_DIRECTORY_ID = 'R';
constexpr char MARKET_MAKER_ID = 'L';

constexpr std::uint64_t MARKET_OPEN_TS = 34200000000000ULL;
constexpr std::uint64_t START_POINT = 600000000000ULL;
constexpr std::uint64_t END_POINT = 4200000000000ULL;

const std::array<const char *, 8> ORDER_SCHEMA = {
    "stockId",  "timestamp", "orderId",     "side",
    "quantity", "price",     "attribution", "prevOrder"};

const std::array<const char *, 5> EXECUTION_SCHEMA = {
    "timestamp", "orderId", "stockId", "quantity", "price"};

const std::array<const char *, 4> CANCELLATION_SCHEMA = {"timestamp", "orderId",
                                                         "stockId", "quantity"};

const std::array<const char *, 15> STOCKS_SCHEMA = {
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
    "InverseIndicator"};

const std::array<const char *, 6> MARKET_MAKER_SCHEMA = {
    "timestamp", "stockId", "name", "isPrimary", "mode", "state"};

struct Order {
  std::uint16_t stockId;
  std::uint64_t timestamp;
  std::uint64_t orderId;
  std::optional<std::string> side;
  std::uint32_t quantity;
  double price;
  std::optional<std::string> attribution;
  std::optional<std::uint64_t> prevOrder;
};

struct Execution {
  std::uint64_t timestamp;
  std::optional<std::uint64_t> orderId;
  std::uint16_t stockId;
  std::uint32_t quantity;
  std::optional<double> price;
};

struct Cancellation {
  std::uint64_t timestamp;
  std::uint64_t orderId;
  std::uint16_t stockId;
  std::optional<std::uint32_t> quantity;
};

struct StockDirectoryEntry {
  std::uint16_t stockId;
  std::string name;
  std::string marketCategory;
  std::string financialStatusIndicator;
  std::uint32_t roundLotSize;
  bool roundLotsOnly;
  std::string issueClassification;
  std::string issueSubType;
  std::string authenticity;
  bool shortSaleThresholdIndicator;
  bool IPOFlag;
  std::string LULDReferencePriceTier;
  bool ETPFlag;
  std::uint32_t ETPLeverageFactor;
  bool InverseIndicator;
};

struct MarketMaker {
  std::uint64_t timestamp;
  std::uint16_t stockId;
  std::string name;
  bool isPrimary;
  std::string mode;
  std::string state;
};

std::string strip_ascii(std::string value) {
  while (!value.empty() && value.back() == ' ') {
    value.pop_back();
  }
  return value;
}

std::uint16_t read_u16(const std::vector<char> &data, std::size_t offset) {
  return (static_cast<std::uint16_t>(static_cast<unsigned char>(data[offset]))
          << 8) |
         static_cast<std::uint16_t>(
             static_cast<unsigned char>(data[offset + 1]));
}

std::uint32_t read_u32(const std::vector<char> &data, std::size_t offset) {
  return (static_cast<std::uint32_t>(static_cast<unsigned char>(data[offset]))
          << 24) |
         (static_cast<std::uint32_t>(
              static_cast<unsigned char>(data[offset + 1]))
          << 16) |
         (static_cast<std::uint32_t>(
              static_cast<unsigned char>(data[offset + 2]))
          << 8) |
         static_cast<std::uint32_t>(
             static_cast<unsigned char>(data[offset + 3]));
}

std::uint64_t read_u48(const std::vector<char> &data, std::size_t offset) {
  std::uint64_t value = 0;
  for (std::size_t i = 0; i < 6; ++i) {
    value = (value << 8) | static_cast<unsigned char>(data[offset + i]);
  }
  return value;
}

std::uint64_t read_u64(const std::vector<char> &data, std::size_t offset) {
  std::uint64_t value = 0;
  for (std::size_t i = 0; i < 8; ++i) {
    value = (value << 8) | static_cast<unsigned char>(data[offset + i]);
  }
  return value;
}

std::string read_string(const std::vector<char> &data, std::size_t offset,
                        std::size_t size) {
  return strip_ascii(std::string(data.data() + offset, size));
}

std::string side_to_string(char side) {
  if (side == 'B') {
    return "BUY";
  }
  if (side == 'S') {
    return "SELL";
  }
  throw std::runtime_error("unexpected side");
}

template <std::size_t N>
void write_header(std::ofstream &file,
                  const std::array<const char *, N> &schema) {
  for (std::size_t i = 0; i < N; ++i) {
    if (i != 0) {
      file << ';';
    }
    file << schema[i];
  }
  file << '\n';
}

std::string python_bool(bool value) { return value ? "True" : "False"; }

std::string format_price(double value) {
  std::ostringstream out;
  out << std::defaultfloat << std::setprecision(15) << value;
  return out.str();
}

void write_order(std::ofstream &file, const Order &order) {
  file << order.stockId << ';' << order.timestamp << ';' << order.orderId
       << ';';
  if (order.side.has_value()) {
    file << *order.side;
  }
  file << ';' << order.quantity << ';' << format_price(order.price) << ';';
  if (order.attribution.has_value()) {
    file << *order.attribution;
  }
  file << ';';
  if (order.prevOrder.has_value()) {
    file << *order.prevOrder;
  }
  file << '\n';
}

void write_execution(std::ofstream &file, const Execution &execution) {
  file << execution.timestamp << ';';
  if (execution.orderId.has_value()) {
    file << *execution.orderId;
  }
  file << ';' << execution.stockId << ';' << execution.quantity << ';';
  if (execution.price.has_value()) {
    file << format_price(*execution.price);
  }
  file << '\n';
}

void write_cancellation(std::ofstream &file, const Cancellation &cancellation) {
  file << cancellation.timestamp << ';' << cancellation.orderId << ';'
       << cancellation.stockId << ';';
  if (cancellation.quantity.has_value()) {
    file << *cancellation.quantity;
  }
  file << '\n';
}

void write_stock(std::ofstream &file, const StockDirectoryEntry &stock) {
  file << stock.stockId << ';' << stock.name << ';' << stock.marketCategory
       << ';' << stock.financialStatusIndicator << ';' << stock.roundLotSize
       << ';' << python_bool(stock.roundLotsOnly) << ';'
       << stock.issueClassification << ';' << stock.issueSubType << ';'
       << stock.authenticity << ';'
       << python_bool(stock.shortSaleThresholdIndicator) << ';'
       << python_bool(stock.IPOFlag) << ';' << stock.LULDReferencePriceTier
       << ';' << python_bool(stock.ETPFlag) << ';' << stock.ETPLeverageFactor
       << ';' << python_bool(stock.InverseIndicator) << '\n';
}

void write_market_maker(std::ofstream &file, const MarketMaker &marketMaker) {
  file << marketMaker.timestamp << ';' << marketMaker.stockId << ';'
       << marketMaker.name << ';' << python_bool(marketMaker.isPrimary) << ';'
       << marketMaker.mode << ';' << marketMaker.state << '\n';
}

MarketMaker handle_market_makers(const std::vector<char> &pkg) {
  return MarketMaker{
      .timestamp = read_u48(pkg, 5),
      .stockId = read_u16(pkg, 1),
      .name = read_string(pkg, 11, 4),
      .isPrimary = pkg[23] == 'Y',
      .mode = read_string(pkg, 24, 1),
      .state = read_string(pkg, 25, 1),
  };
}

StockDirectoryEntry handle_stock_directory(const std::vector<char> &pkg) {
  return StockDirectoryEntry{
      .stockId = read_u16(pkg, 1),
      .name = read_string(pkg, 11, 8),
      .marketCategory = read_string(pkg, 19, 1),
      .financialStatusIndicator = read_string(pkg, 20, 1),
      .roundLotSize = read_u32(pkg, 21),
      .roundLotsOnly = pkg[25] == 'Y',
      .issueClassification = read_string(pkg, 26, 1),
      .issueSubType = read_string(pkg, 27, 2),
      .authenticity = read_string(pkg, 29, 1),
      .shortSaleThresholdIndicator = pkg[30] == 'Y',
      .IPOFlag = pkg[31] == 'Y',
      .LULDReferencePriceTier = read_string(pkg, 32, 1),
      .ETPFlag = pkg[33] == 'Y',
      .ETPLeverageFactor = read_u32(pkg, 34),
      .InverseIndicator = pkg[38] == 'Y',
  };
}

Order handle_order_add(const std::vector<char> &pkg) {
  return Order{
      .stockId = read_u16(pkg, 1),
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .side = side_to_string(pkg[19]),
      .quantity = read_u32(pkg, 20),
      .price = static_cast<double>(read_u32(pkg, 32)) / 10000.0,
      .attribution = std::nullopt,
      .prevOrder = std::nullopt,
  };
}

Order handle_order_add_with_attribution(const std::vector<char> &pkg) {
  return Order{
      .stockId = read_u16(pkg, 1),
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .side = side_to_string(pkg[19]),
      .quantity = read_u32(pkg, 20),
      .price = static_cast<double>(read_u32(pkg, 32)) / 10000.0,
      .attribution = read_string(pkg, 36, 4),
      .prevOrder = std::nullopt,
  };
}

Execution handle_order_execute(const std::vector<char> &pkg) {
  return Execution{
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .stockId = read_u16(pkg, 1),
      .quantity = read_u32(pkg, 19),
      .price = std::nullopt,
  };
}

Execution handle_order_execute_with_price(const std::vector<char> &pkg) {
  return Execution{
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .stockId = read_u16(pkg, 1),
      .quantity = read_u32(pkg, 19),
      .price = static_cast<double>(read_u32(pkg, 32)) / 10000.0,
  };
}

Execution handle_trade(const std::vector<char> &pkg) {
  return Execution{
      .timestamp = read_u48(pkg, 5),
      .orderId = std::nullopt,
      .stockId = read_u16(pkg, 1),
      .quantity = read_u32(pkg, 20),
      .price = static_cast<double>(read_u32(pkg, 32)) / 10000.0,
  };
}

Cancellation handle_order_cancel(const std::vector<char> &pkg) {
  return Cancellation{
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .stockId = read_u16(pkg, 1),
      .quantity = read_u32(pkg, 19),
  };
}

Cancellation handle_order_delete(const std::vector<char> &pkg) {
  return Cancellation{
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .stockId = read_u16(pkg, 1),
      .quantity = std::nullopt,
  };
}

Order handle_order_replace(const std::vector<char> &pkg) {
  return Order{
      .stockId = read_u16(pkg, 1),
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 19),
      .side = std::nullopt,
      .quantity = read_u32(pkg, 27),
      .price = static_cast<double>(read_u32(pkg, 31)) / 10000.0,
      .attribution = std::nullopt,
      .prevOrder = read_u64(pkg, 11),
  };
}

} // namespace

int main(int argc, char *argv[]) {
  if (argc != 3) {
    std::cerr << "Usage: " << argv[0] << " <dumpFile> <outputDir>\n";
    return 1;
  }

  const std::filesystem::path source_file = argv[1];
  const std::filesystem::path output_dir = argv[2];
  std::filesystem::create_directories(output_dir);

  std::ofstream order_file(output_dir / "orders.csv");
  std::ofstream order_premarket_file(output_dir / "ordersPreMarket.csv");
  std::ofstream execution_file(output_dir / "executions.csv");
  std::ofstream execution_premarket_file(output_dir /
                                         "executionsPreMarket.csv");
  std::ofstream cancellation_file(output_dir / "cancellations.csv");
  std::ofstream cancellation_premarket_file(output_dir /
                                            "cancellationsPreMarket.csv");
  std::ofstream stocks_file(output_dir / "stocks.csv");
  std::ofstream market_maker_file(output_dir / "marketMakers.csv");

  write_header(order_file, ORDER_SCHEMA);
  write_header(order_premarket_file, ORDER_SCHEMA);
  write_header(execution_file, EXECUTION_SCHEMA);
  write_header(execution_premarket_file, EXECUTION_SCHEMA);
  write_header(cancellation_file, CANCELLATION_SCHEMA);
  write_header(cancellation_premarket_file, CANCELLATION_SCHEMA);
  write_header(stocks_file, STOCKS_SCHEMA);
  write_header(market_maker_file, MARKET_MAKER_SCHEMA);

  std::ifstream input(source_file, std::ios::binary | std::ios::ate);
  if (!input) {
    std::cerr << "Failed to open " << source_file << '\n';
    return 1;
  }

  const auto size = input.tellg();
  input.seekg(0, std::ios::beg);

  std::vector<char> file_content(static_cast<std::size_t>(size));
  if (!input.read(file_content.data(), size)) {
    std::cerr << "Failed to read " << source_file << '\n';
    return 1;
  }

  std::size_t offset = 0;
  std::uint64_t msg_count = 0;
  std::unordered_set<std::uint16_t> seen_stocks;

  while (offset < file_content.size()) {
    const std::uint16_t msg_len = read_u16(file_content, offset);
    const char msg_type = file_content[offset + 2];
    const std::vector<char> pkg(
        file_content.begin() + static_cast<std::ptrdiff_t>(offset + 2),
        file_content.begin() +
            static_cast<std::ptrdiff_t>(offset + msg_len + 2));

    std::optional<Order> order;
    std::optional<Cancellation> cancellation;
    std::optional<Execution> execution;

    if (msg_type == STOCK_DIRECTORY_ID) {
      const auto directory_entry = handle_stock_directory(pkg);
      if (!seen_stocks.contains(directory_entry.stockId)) {
        write_stock(stocks_file, directory_entry);
        seen_stocks.insert(directory_entry.stockId);
      }
    } else if (msg_type == MARKET_MAKER_ID) {
      write_market_maker(market_maker_file, handle_market_makers(pkg));
    } else if (msg_type == ORDER_ADD_ID) {
      order = handle_order_add(pkg);
    } else if (msg_type == ORDER_ADD_WITH_MPID_ID) {
      order = handle_order_add_with_attribution(pkg);
    } else if (msg_type == ORDER_REPLACE_ID) {
      order = handle_order_replace(pkg);
    } else if (msg_type == ORDER_CANCEL_ID) {
      cancellation = handle_order_cancel(pkg);
    } else if (msg_type == ORDER_DELETE_ID) {
      cancellation = handle_order_delete(pkg);
    } else if (msg_type == ORDER_EXECUTE_ID) {
      execution = handle_order_execute(pkg);
    } else if (msg_type == ORDER_EXECUTE_WITH_PRICE_ID) {
      execution = handle_order_execute_with_price(pkg);
    } else if (msg_type == TRADE_ID) {
      execution = handle_trade(pkg);
    }

    const auto cutoff_start = MARKET_OPEN_TS + START_POINT;
    const auto cutoff_end = MARKET_OPEN_TS + END_POINT;

    if (order.has_value()) {
      if (order->timestamp < cutoff_start) {
        write_order(order_premarket_file, *order);
      } else if (order->timestamp > cutoff_end) {
        break;
      } else {
        write_order(order_file, *order);
      }
    }

    if (execution.has_value()) {
      if (execution->timestamp < cutoff_start) {
        write_execution(execution_premarket_file, *execution);
      } else if (execution->timestamp > cutoff_end) {
        break;
      } else {
        write_execution(execution_file, *execution);
      }
    }

    if (cancellation.has_value()) {
      if (cancellation->timestamp < cutoff_start) {
        write_cancellation(cancellation_premarket_file, *cancellation);
      } else if (cancellation->timestamp > cutoff_end) {
        break;
      } else {
        write_cancellation(cancellation_file, *cancellation);
      }
    }

    offset += msg_len + 2;
    ++msg_count;
    if (msg_count % 1000000 == 0) {
      std::cout << "Parsed " << msg_count << " messages. At offset " << offset
                << "/" << file_content.size() << " (" << std::fixed
                << std::setprecision(2)
                << (static_cast<double>(offset) /
                    static_cast<double>(file_content.size()) * 100.0)
                << "%)\n";
    }
  }

  return 0;
}
