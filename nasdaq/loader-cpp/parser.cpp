#include <algorithm>
#include <array>
#include <charconv>
#include <cstdint>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <optional>
#include <sstream>
#include <stdexcept>
#include <string>
#include <string_view>
#include <unordered_set>
#include <vector>

#include <fcntl.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>

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

class MappedFile {
public:
  explicit MappedFile(const std::filesystem::path &path) {
    fd_ = open(path.c_str(), O_RDONLY);
    if (fd_ == -1) {
      throw std::runtime_error("Failed to open " + path.string());
    }

    struct stat st{};
    if (fstat(fd_, &st) == -1) {
      close(fd_);
      throw std::runtime_error("Failed to stat " + path.string());
    }

    size_ = static_cast<std::size_t>(st.st_size);
    if (size_ == 0) {
      return;
    }

    void *mapping = mmap(nullptr, size_, PROT_READ, MAP_PRIVATE, fd_, 0);
    if (mapping == MAP_FAILED) {
      close(fd_);
      throw std::runtime_error("Failed to mmap " + path.string());
    }

    data_ = static_cast<const char *>(mapping);
    madvise(const_cast<char *>(data_), size_, MADV_SEQUENTIAL);
  }

  MappedFile(const MappedFile &) = delete;
  MappedFile &operator=(const MappedFile &) = delete;

  ~MappedFile() {
    if (data_ != nullptr) {
      munmap(const_cast<char *>(data_), size_);
    }
    if (fd_ != -1) {
      close(fd_);
    }
  }

  const char *data() const { return data_; }
  std::size_t size() const { return size_; }

private:
  int fd_ = -1;
  const char *data_ = nullptr;
  std::size_t size_ = 0;
};

class BufferedCsvWriter {
public:
  explicit BufferedCsvWriter(const std::filesystem::path &path,
                             std::size_t flush_threshold = 1 << 20)
      : file_(path, std::ios::binary), flush_threshold_(flush_threshold) {
    if (!file_) {
      throw std::runtime_error("Failed to open " + path.string());
    }
    buffer_.reserve(flush_threshold_);
  }

  BufferedCsvWriter(const BufferedCsvWriter &) = delete;
  BufferedCsvWriter &operator=(const BufferedCsvWriter &) = delete;

  ~BufferedCsvWriter() {
    try {
      flush();
    } catch (...) {
    }
  }

  void append(std::string_view value) {
    buffer_.append(value.data(), value.size());
    flush_if_needed();
  }

  void append(const char *value) {
    buffer_ += value;
    flush_if_needed();
  }

  void append_char(char value) {
    buffer_.push_back(value);
    flush_if_needed();
  }

  void flush() {
    if (buffer_.empty()) {
      return;
    }
    file_.write(buffer_.data(), static_cast<std::streamsize>(buffer_.size()));
    if (!file_) {
      throw std::runtime_error("Failed to write CSV output");
    }
    buffer_.clear();
  }

private:
  void flush_if_needed() {
    if (buffer_.size() >= flush_threshold_) {
      flush();
    }
  }

  std::ofstream file_;
  std::string buffer_;
  std::size_t flush_threshold_;
};

struct Order {
  std::uint16_t stockId;
  std::uint64_t timestamp;
  std::uint64_t orderId;
  std::optional<char> side;
  std::uint32_t quantity;
  std::uint32_t price;
  std::optional<std::string> attribution;
  std::optional<std::uint64_t> prevOrder;
};

struct Execution {
  std::uint64_t timestamp;
  std::optional<std::uint64_t> orderId;
  std::uint16_t stockId;
  std::uint32_t quantity;
  std::optional<std::uint32_t> price;
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

std::uint16_t read_u16(const char *data, std::size_t offset) {
  return (static_cast<std::uint16_t>(static_cast<unsigned char>(data[offset]))
          << 8) |
         static_cast<std::uint16_t>(
             static_cast<unsigned char>(data[offset + 1]));
}

std::uint32_t read_u32(const char *data, std::size_t offset) {
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

std::uint64_t read_u48(const char *data, std::size_t offset) {
  std::uint64_t value = 0;
  for (std::size_t i = 0; i < 6; ++i) {
    value = (value << 8) | static_cast<unsigned char>(data[offset + i]);
  }
  return value;
}

std::uint64_t read_u64(const char *data, std::size_t offset) {
  std::uint64_t value = 0;
  for (std::size_t i = 0; i < 8; ++i) {
    value = (value << 8) | static_cast<unsigned char>(data[offset + i]);
  }
  return value;
}

std::string read_string(const char *data, std::size_t offset,
                        std::size_t size) {
  return strip_ascii(std::string(data + offset, size));
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

template <typename T> void append_integer(std::string &buffer, T value) {
  char digits[32];
  const auto [ptr, ec] = std::to_chars(std::begin(digits), std::end(digits), value);
  if (ec != std::errc()) {
    throw std::runtime_error("Failed to format integer");
  }
  buffer.append(digits, ptr);
}

void append_python_bool(std::string &buffer, bool value) {
  buffer += value ? "True" : "False";
}

void append_price(std::string &buffer, std::uint32_t value) {
  append_integer(buffer, value / 10000);
  const auto fractional_value = value % 10000;
  if (fractional_value == 0) {
    return;
  }

  char fractional[4] = {'0', '0', '0', '0'};
  char digits[4];
  const auto [ptr, ec] =
      std::to_chars(std::begin(digits), std::end(digits), fractional_value);
  if (ec != std::errc()) {
    throw std::runtime_error("Failed to format price");
  }

  const auto digits_len = static_cast<std::size_t>(ptr - digits);
  std::copy(digits, ptr, fractional + (4 - digits_len));

  std::size_t fractional_len = 4;
  while (fractional_len > 0 && fractional[fractional_len - 1] == '0') {
    --fractional_len;
  }

  buffer.push_back('.');
  buffer.append(fractional, fractional_len);
}

template <std::size_t N>
void write_header(BufferedCsvWriter &file,
                  const std::array<const char *, N> &schema) {
  for (std::size_t i = 0; i < N; ++i) {
    if (i != 0) {
      file.append_char(';');
    }
    file.append(schema[i]);
  }
  file.append_char('\n');
}

void write_order(BufferedCsvWriter &file, const Order &order) {
  auto row = std::string{};
  row.reserve(96);
  append_integer(row, order.stockId);
  row.push_back(';');
  append_integer(row, order.timestamp);
  row.push_back(';');
  append_integer(row, order.orderId);
  row.push_back(';');
  if (order.side.has_value()) {
    row += side_to_string(*order.side);
  }
  row.push_back(';');
  append_integer(row, order.quantity);
  row.push_back(';');
  append_price(row, order.price);
  row.push_back(';');
  if (order.attribution.has_value()) {
    row += *order.attribution;
  }
  row.push_back(';');
  if (order.prevOrder.has_value()) {
    append_integer(row, *order.prevOrder);
  }
  row.push_back('\n');
  file.append(row);
}

void write_execution(BufferedCsvWriter &file, const Execution &execution) {
  auto row = std::string{};
  row.reserve(64);
  append_integer(row, execution.timestamp);
  row.push_back(';');
  if (execution.orderId.has_value()) {
    append_integer(row, *execution.orderId);
  }
  row.push_back(';');
  append_integer(row, execution.stockId);
  row.push_back(';');
  append_integer(row, execution.quantity);
  row.push_back(';');
  if (execution.price.has_value()) {
    append_price(row, *execution.price);
  }
  row.push_back('\n');
  file.append(row);
}

void write_cancellation(BufferedCsvWriter &file,
                        const Cancellation &cancellation) {
  auto row = std::string{};
  row.reserve(64);
  append_integer(row, cancellation.timestamp);
  row.push_back(';');
  append_integer(row, cancellation.orderId);
  row.push_back(';');
  append_integer(row, cancellation.stockId);
  row.push_back(';');
  if (cancellation.quantity.has_value()) {
    append_integer(row, *cancellation.quantity);
  }
  row.push_back('\n');
  file.append(row);
}

void write_stock(BufferedCsvWriter &file, const StockDirectoryEntry &stock) {
  auto row = std::string{};
  row.reserve(128);
  append_integer(row, stock.stockId);
  row.push_back(';');
  row += stock.name;
  row.push_back(';');
  row += stock.marketCategory;
  row.push_back(';');
  row += stock.financialStatusIndicator;
  row.push_back(';');
  append_integer(row, stock.roundLotSize);
  row.push_back(';');
  append_python_bool(row, stock.roundLotsOnly);
  row.push_back(';');
  row += stock.issueClassification;
  row.push_back(';');
  row += stock.issueSubType;
  row.push_back(';');
  row += stock.authenticity;
  row.push_back(';');
  append_python_bool(row, stock.shortSaleThresholdIndicator);
  row.push_back(';');
  append_python_bool(row, stock.IPOFlag);
  row.push_back(';');
  row += stock.LULDReferencePriceTier;
  row.push_back(';');
  append_python_bool(row, stock.ETPFlag);
  row.push_back(';');
  append_integer(row, stock.ETPLeverageFactor);
  row.push_back(';');
  append_python_bool(row, stock.InverseIndicator);
  row.push_back('\n');
  file.append(row);
}

void write_market_maker(BufferedCsvWriter &file,
                        const MarketMaker &marketMaker) {
  auto row = std::string{};
  row.reserve(64);
  append_integer(row, marketMaker.timestamp);
  row.push_back(';');
  append_integer(row, marketMaker.stockId);
  row.push_back(';');
  row += marketMaker.name;
  row.push_back(';');
  append_python_bool(row, marketMaker.isPrimary);
  row.push_back(';');
  row += marketMaker.mode;
  row.push_back(';');
  row += marketMaker.state;
  row.push_back('\n');
  file.append(row);
}

MarketMaker handle_market_makers(const char *pkg) {
  return MarketMaker{
      .timestamp = read_u48(pkg, 5),
      .stockId = read_u16(pkg, 1),
      .name = read_string(pkg, 11, 4),
      .isPrimary = pkg[23] == 'Y',
      .mode = read_string(pkg, 24, 1),
      .state = read_string(pkg, 25, 1),
  };
}

StockDirectoryEntry handle_stock_directory(const char *pkg) {
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

Order handle_order_add(const char *pkg) {
  return Order{
      .stockId = read_u16(pkg, 1),
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .side = pkg[19],
      .quantity = read_u32(pkg, 20),
      .price = read_u32(pkg, 32),
      .attribution = std::nullopt,
      .prevOrder = std::nullopt,
  };
}

Order handle_order_add_with_attribution(const char *pkg) {
  return Order{
      .stockId = read_u16(pkg, 1),
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .side = pkg[19],
      .quantity = read_u32(pkg, 20),
      .price = read_u32(pkg, 32),
      .attribution = read_string(pkg, 36, 4),
      .prevOrder = std::nullopt,
  };
}

Execution handle_order_execute(const char *pkg) {
  return Execution{
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .stockId = read_u16(pkg, 1),
      .quantity = read_u32(pkg, 19),
      .price = std::nullopt,
  };
}

Execution handle_order_execute_with_price(const char *pkg) {
  return Execution{
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .stockId = read_u16(pkg, 1),
      .quantity = read_u32(pkg, 19),
      .price = read_u32(pkg, 32),
  };
}

Execution handle_trade(const char *pkg) {
  return Execution{
      .timestamp = read_u48(pkg, 5),
      .orderId = std::nullopt,
      .stockId = read_u16(pkg, 1),
      .quantity = read_u32(pkg, 20),
      .price = read_u32(pkg, 32),
  };
}

Cancellation handle_order_cancel(const char *pkg) {
  return Cancellation{
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .stockId = read_u16(pkg, 1),
      .quantity = read_u32(pkg, 19),
  };
}

Cancellation handle_order_delete(const char *pkg) {
  return Cancellation{
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 11),
      .stockId = read_u16(pkg, 1),
      .quantity = std::nullopt,
  };
}

Order handle_order_replace(const char *pkg) {
  return Order{
      .stockId = read_u16(pkg, 1),
      .timestamp = read_u48(pkg, 5),
      .orderId = read_u64(pkg, 19),
      .side = std::nullopt,
      .quantity = read_u32(pkg, 27),
      .price = read_u32(pkg, 31),
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

  BufferedCsvWriter order_file(output_dir / "orders.csv");
  BufferedCsvWriter order_premarket_file(output_dir / "ordersPreMarket.csv");
  BufferedCsvWriter execution_file(output_dir / "executions.csv");
  BufferedCsvWriter execution_premarket_file(output_dir /
                                             "executionsPreMarket.csv");
  BufferedCsvWriter cancellation_file(output_dir / "cancellations.csv");
  BufferedCsvWriter cancellation_premarket_file(output_dir /
                                                "cancellationsPreMarket.csv");
  BufferedCsvWriter stocks_file(output_dir / "stocks.csv");
  BufferedCsvWriter market_maker_file(output_dir / "marketMakers.csv");

  write_header(order_file, ORDER_SCHEMA);
  write_header(order_premarket_file, ORDER_SCHEMA);
  write_header(execution_file, EXECUTION_SCHEMA);
  write_header(execution_premarket_file, EXECUTION_SCHEMA);
  write_header(cancellation_file, CANCELLATION_SCHEMA);
  write_header(cancellation_premarket_file, CANCELLATION_SCHEMA);
  write_header(stocks_file, STOCKS_SCHEMA);
  write_header(market_maker_file, MARKET_MAKER_SCHEMA);

  MappedFile input(source_file);

  std::size_t offset = 0;
  std::uint64_t msg_count = 0;
  std::unordered_set<std::uint16_t> seen_stocks;
  const char *file_data = input.data();

  const auto cutoff_start = MARKET_OPEN_TS + START_POINT;
  const auto cutoff_end = MARKET_OPEN_TS + END_POINT;

  while (offset < input.size()) {
    const std::uint16_t msg_len = read_u16(file_data, offset);
    const char *pkg = file_data + offset + 2;
    const char msg_type = pkg[0];

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
                << "/" << input.size() << " (" << std::fixed
                << std::setprecision(2)
                << (static_cast<double>(offset) /
                    static_cast<double>(input.size()) * 100.0)
                << "%)\n";
    }
  }

  order_file.flush();
  order_premarket_file.flush();
  execution_file.flush();
  execution_premarket_file.flush();
  cancellation_file.flush();
  cancellation_premarket_file.flush();
  stocks_file.flush();
  market_maker_file.flush();

  return 0;
}
