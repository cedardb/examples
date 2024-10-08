#include <algorithm>
#include <array>
#include <chrono>
#include <iostream>
#include <random>
#include <thread>
#include <vector>
// ----------------------------------------------------------------------------
using series = uint64_t;
using decimal = uint64_t;
// ----------------------------------------------------------------------------
// Represent all elements of a row consecutively in memory
struct Employee {
    series id;
    decimal salary;
    std::array<char, 16> firstname;
};
// Vector is a growable array of data, each element is a full row
struct ArrayOfStruct {
    // The rows
    std::vector<Employee> table;

    // Calculates the size
    size_t getSize() const {
        return table.size() * sizeof(decltype(table)::value_type) + sizeof(ArrayOfStruct);
    }
};
// ----------------------------------------------------------------------------
// Store the same column consecutively in memory
// For simplicity we ignore, that vectors put the data onto different memory regions.
// A database would most likely use a custom data structure that packs all columns.
struct StructOfArrays {
    // The columns
    std::vector<series> id;
    std::vector<decimal> salary;
    std::vector<std::array<char, 16>> firstname;

    // Calculates the size
    size_t getSize() const {
        size_t size = id.size() * sizeof(decltype(id)::value_type);
        size += salary.size() * sizeof(decltype(salary)::value_type);
        size += firstname.size() * sizeof(decltype(firstname)::value_type);
        return size + sizeof(StructOfArrays);
    }
};
// ----------------------------------------------------------------------------
// The different compression algorithms
enum class CompressionType : uint8_t {
    RAW,
    FOR1,
    FOR2,
    FOR4
};
// ----------------------------------------------------------------------------
// Base class for raw and compressed columns
template <typename T>
struct Column {
    using value_type = T;
    // The compression scheme
    CompressionType type;
    // Destructor
    virtual ~Column() {}
    // Constructor
    Column(CompressionType type) : type(type) {}
    // Calculate the size (virtual)
    virtual size_t getSize() const = 0;
};
// ---------------------------------------------------------------------------
// Column stored in raw format, i.e. vector<type>
template <typename T>
struct RawColumn : public Column<T> {
    // The raw values
    std::vector<T> column;

    // Constructor
    RawColumn(auto begin, auto end) : Column<T>(CompressionType::RAW) {
        column.reserve(end - begin);
        for (auto it = begin; it != end; ++it)
            column.emplace_back(*it);
    }
    // Destructor
    ~RawColumn() override = default;
    // Calculates the size
    size_t getSize() const override {
        return column.size() * sizeof(typename decltype(column)::value_type) + sizeof(RawColumn<T>);
    }
};
// ----------------------------------------------------------------------------
// Column compressed with frame-of-reference encoding
template <typename T, typename C>
requires std::is_unsigned_v<T>
struct FORCompressedColumn : public Column<T> {
    // The minimum value of the column
    T minValue;
    // The maximum value of the column (not necessary for FOR, but useful later on)
    T maxValue;
    // The FOR compressed values
    std::vector<C> compressed;

    // Constructor
    FORCompressedColumn(auto begin, auto end, T minValue, T maxValue, CompressionType type) : Column<T>(type), minValue(minValue), maxValue(maxValue) {
        compressed.reserve(end - begin);
        for (auto it = begin; it != end; ++it)
            compressed.emplace_back(*it - minValue);
    }
    // Destructor
    ~FORCompressedColumn() override = default;
    // Calculates the size
    size_t getSize() const override {
        return compressed.size() * sizeof(typename decltype(compressed)::value_type) + sizeof(FORCompressedColumn<T, C>);
    }
};
// ----------------------------------------------------------------------------
// Store the same column with compression
struct CompressedStructOfArrays {
    // The potentially compressed columns
    std::unique_ptr<Column<series>> id;
    std::unique_ptr<Column<decimal>> salary;
    std::unique_ptr<Column<std::array<char, 16>>> firstname;

    // Calculates the size
    size_t getSize() const {
        return id->getSize() + salary->getSize() + firstname->getSize() + sizeof(CompressedStructOfArrays);
    }
};
// ---------------------------------------------------------------------------
static constexpr auto createColumn(auto begin, auto end) -> std::unique_ptr<Column<typename decltype(begin)::value_type>>
// Compute the perfect compression scheme according to values
{
    using T = decltype(begin)::value_type;
    if constexpr (!std::is_unsigned_v<T>) {
        return std::make_unique<RawColumn<T>>(begin, end);
    } else {
        auto [minValue, maxValue] = std::ranges::minmax_element(begin, end);
        if (*maxValue - *minValue > std::numeric_limits<uint32_t>::max())
            return std::make_unique<RawColumn<T>>(begin, end);

        if (*maxValue - *minValue <= std::numeric_limits<uint8_t>::max()) {
            return std::make_unique<FORCompressedColumn<T, uint8_t>>(begin, end, *minValue, *maxValue, CompressionType::FOR1);
        } else if (maxValue - minValue <= std::numeric_limits<uint16_t>::max()) {
            return std::make_unique<FORCompressedColumn<T, uint16_t>>(begin, end, *minValue, *maxValue, CompressionType::FOR2);
        } else {
            return std::make_unique<FORCompressedColumn<T, uint32_t>>(begin, end, *minValue, *maxValue, CompressionType::FOR4);
        }
    }
}
// ----------------------------------------------------------------------------
// Store the same columns now in PAX format
struct PaxStore {
    // The blocks of compressed columns
    std::vector<CompressedStructOfArrays> pax;

    // Calculates the size
    size_t getSize() const {
        size_t size = sizeof(PaxStore);
        for (const auto& elem : pax)
            size += elem.getSize();
        return size;
    }
};
// ---------------------------------------------------------------------------
static PaxStore createPaxColumn(StructOfArrays& store, size_t paxSize)
// Compute the perfect compression scheme according to values
{
    PaxStore pax;
    pax.pax.reserve(store.id.size() / paxSize + 1);
    for (size_t i = 0; i < store.id.size(); i += paxSize) {
        CompressedStructOfArrays c;
        if (i + paxSize > store.id.size()) {
            c.id = createColumn(store.id.begin() + i, store.id.end());
            c.salary = createColumn(store.salary.begin() + i, store.salary.end());
            c.firstname = createColumn(store.firstname.begin() + i, store.firstname.end());
        } else {
            c.id = createColumn(store.id.begin() + i, store.id.begin() + i + paxSize);
            c.salary = createColumn(store.salary.begin() + i, store.salary.begin() + i + paxSize);
            c.firstname = createColumn(store.firstname.begin() + i, store.firstname.begin() + i + paxSize);
        }
        pax.pax.emplace_back(std::move(c));
    }
    return pax;
}
// ----------------------------------------------------------------------------
static decimal getTotalSalary(const ArrayOfStruct& store)
// Computes the salary
{
    decimal salary = 0;
    // Iterate over the rows and select the salary
    for (const auto& r : store.table)
        salary += r.salary;
    return salary;
}
// ----------------------------------------------------------------------------
static decimal getTotalSalary(const StructOfArrays& store)
// Computes the salary
{
    decimal salary = 0;
    // Iterate over the column
    for (auto s : store.salary)
        salary += s;
    return salary;
}
// ----------------------------------------------------------------------------
static decimal getTotalSalary(const CompressedStructOfArrays& store)
// Computes the salary
{
    auto forComputation = []<typename T>(const T& column) {
        decimal salary = 0;
        for (auto s : column.compressed) {
            salary += s + column.minValue;
        }
        return salary;
    };

    using T = decltype(store.salary)::element_type::value_type;
    decimal salary = 0;
    // Iterate over the column
    switch (store.salary->type) {
        case CompressionType::RAW:
            {
                auto column = static_cast<RawColumn<T>*>(store.salary.get());
                for (auto s : column->column) {
                    salary += s;
                }
                break;
            }
        case CompressionType::FOR1:
            return forComputation(*static_cast<FORCompressedColumn<T, uint8_t>*>(store.salary.get()));
        case CompressionType::FOR2:
            return forComputation(*static_cast<FORCompressedColumn<T, uint16_t>*>(store.salary.get()));
        case CompressionType::FOR4:
            return forComputation(*static_cast<FORCompressedColumn<T, uint32_t>*>(store.salary.get()));
    }
    return salary;
}
// ----------------------------------------------------------------------------
static decimal getTotalSalary(const PaxStore& store)
// Computes the salary
{
    decimal salary = 0;
    // Iterate over the column
    for (const auto& p : store.pax)
        salary += getTotalSalary(p);
    return salary;
}
// ----------------------------------------------------------------------------
static void updateEntry(ArrayOfStruct& store, std::vector<size_t> ids, std::array<char, 16>& newName)
// Update entries
{
    for (auto id : ids) {
        auto& record = store.table[id];
        record.firstname = newName;
        record.salary *= 2;
    }
}
// ----------------------------------------------------------------------------
static void updateEntry(StructOfArrays& store, std::vector<size_t> ids, std::array<char, 16>& newName)
// Update entries
{
    for (auto id : ids) {
        store.firstname[id] = newName;
        store.salary[id] = 2 * store.salary[id];
    }
}
// ----------------------------------------------------------------------------
int main() {
    // Create 100M Users
    auto users = 100 * 1000 * 1000ull;
    std::array<char, 16> name = {"Moritz - Felipe"};
    // For both versions
    ArrayOfStruct aos;
    StructOfArrays soa;
    CompressedStructOfArrays compressed;
    PaxStore pax;
    // Fill the stores
    for (auto i = 0ull; i < users; i++) {
        aos.table.emplace_back(static_cast<unsigned>(i), (1000 + (static_cast<decimal>(i) % 500)) * 100, name);

        soa.id.push_back(static_cast<unsigned>(i));
        soa.salary.push_back((1000 + (static_cast<decimal>(i) % 500)) * 100);
        soa.firstname.emplace_back(name);
    }

    compressed.id = createColumn(soa.id.cbegin(), soa.id.cend());
    compressed.salary = createColumn(soa.salary.cbegin(), soa.salary.cend());
    compressed.firstname = createColumn(soa.firstname.cbegin(), soa.firstname.cend());

    pax = createPaxColumn(soa, std::numeric_limits<uint16_t>::max());

    // Computes the salary
    auto measureSalary = []<typename T>(const T& store) {
        // Start the timer
        std::chrono::steady_clock::time_point begin = std::chrono::steady_clock::now();
        auto salary = getTotalSalary(store);
        // Stop the timer
        std::chrono::steady_clock::time_point end = std::chrono::steady_clock::now();
        // Determine store kind
        auto storeName = "compressed";
        if constexpr (std::is_same_v<ArrayOfStruct, T>)
            storeName = "aos";
        if constexpr (std::is_same_v<StructOfArrays, T>)
            storeName = "soa";
        if constexpr (std::is_same_v<PaxStore, T>)
            storeName = "pax";
        // Print results
        std::cout << "Workload: Analyze; Store: " << storeName << "; Time: " << std::chrono::duration_cast<std::chrono::microseconds>(end - begin).count() << " [μs]; Size: " << store.getSize() / (1024 * 1024) << " [MB]" << std::endl;
        std::this_thread::sleep_for(std::chrono::milliseconds(1000));
        return salary;
    };

    // Check correctness
    decimal salary = 0;
    if (salary = measureSalary(soa); salary != measureSalary(compressed))
        return -1;

    // Check correctness
    if (salary != measureSalary(pax))
        return -1;

    // Check correctness
    if (salary != measureSalary(aos))
        return -1;

    // -------------

    std::random_device rnd_device;
    std::mt19937 mt{rnd_device()}; // Generates random integers
    std::uniform_int_distribution<uint64_t> dist{0, users - 1};

    auto gen = [&]() {
        return dist(mt);
    };

    std::vector<uint64_t> ids(0.1 * users);
    std::generate(ids.begin(), ids.end(), gen);

    // Updates the column
    auto measureUpdate = []<typename T>(T& store, std::vector<uint64_t> ids) {
        // Update name and salary
        std::array<char, 16> name = {"Dr. Moritz - F."};

        // Start the timer
        std::chrono::steady_clock::time_point begin = std::chrono::steady_clock::now();
        updateEntry(store, ids, name);
        // Stop the timer
        std::chrono::steady_clock::time_point end = std::chrono::steady_clock::now();
        // Determine store kind
        auto storeName = "soa";
        if constexpr (std::is_same_v<ArrayOfStruct, T>)
            storeName = "aos";
        // Print results
        std::cout << "Workload: Update; Store: " << storeName << "; Time: " << std::chrono::duration_cast<std::chrono::microseconds>(end - begin).count() << " [μs]" << std::endl;
    };

    measureUpdate(aos, ids);
    measureUpdate(soa, ids);

    return 0;
}
// ----------------------------------------------------------------------------
