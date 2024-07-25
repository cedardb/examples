#include "PerfEvent.hpp"
#include <iostream>
#include <random>
#include <unordered_set>
#include <vector>
//------------------------------------------------------------------------
// Microbenchmark

unsigned count(const std::vector<unsigned>& input, const std::vector<std::pair<unsigned, unsigned>>& ranges) {
   unsigned res = 0;

   for (const auto& i : input) {
      for (const auto& range : ranges) {
         if (i >= range.first && i <= range.second) res++;
      }
   }

   return res;
}

unsigned countSwappedLoops(const std::vector<unsigned>& input, const std::vector<std::pair<unsigned, unsigned>>& ranges) {
   unsigned res = 0;

   for (const auto& range : ranges) {
      for (const auto& i : input) {
         if (i >= range.first && i <= range.second) res++;
      }
   }

   return res;
}

void microBenchmark() {
   // Fix the benchmark parameters
   auto inputSize = 1'000'000'000u;

   std::vector<std::pair<unsigned, unsigned>> ranges{{5, 6}, {30, 60}};

   // Prepare the result
   auto res = 0u;

   // Prepare random number generation to fill input
   std::mt19937 gen(42);
   std::uniform_int_distribution<unsigned> uniform_dist(1, 100);

   auto numWarmups = 3u;
   auto numRuns = 3u;

   // Prepare space for the input data
   std::vector<unsigned> input;
   input.reserve(inputSize);

   // Fill input with ramdom integers between 0 and 100
   for (auto i = 0; i < inputSize; i++)
      input.push_back(uniform_dist(gen));

   auto benchmarkCount = [&]() {
      res = count(input, ranges);
   };

   auto benchmarkCountSwappedLoops = [&]() {
      res = countSwappedLoops(input, ranges);
   };

   for (auto i = 0; i < numWarmups; i++) benchmarkCount();
   {
      std::cout << "Count: " << std::endl;
      PerfEventBlock e(inputSize / 100);
      benchmarkCount();
      e.e->stopCounters();
      std::cout << "Result: " << res << std::endl;
      std::cout << "Stats: " << std::endl;
   }
   std::cout << std::endl
             << std::endl
             << std::endl;

   for (auto i = 0; i < numWarmups; i++) benchmarkCountSwappedLoops();
   {
      std::cout << "Count with swapped loops: " << std::endl;
      PerfEventBlock e(inputSize / 100);
      benchmarkCountSwappedLoops();
      e.e->stopCounters();
      std::cout << "Result: " << res << std::endl;
      std::cout << "Stats: " << std::endl;
   }
}

//------------------------------------------------------------------------
// Query Benchmark

//------------------------------------------------------------------------
// DuckDB style methods

// Semi Join, probing each tuple against the joinTable and adding tuples with hits to the result
std::vector<std::pair<unsigned, double>> semiJoin(const std::vector<std::pair<unsigned, double>>& input,
                                                  const std::unordered_set<unsigned>& joinTable,
                                                  double selectivityEstimate) {
   std::vector<std::pair<unsigned, double>> result;
   result.reserve(input.size() * selectivityEstimate);
   for (const auto& p : input)
      if (joinTable.contains(p.first))
         result.push_back(p);
   return result;
}

// Filter, specialized for a greater or equal on doubles, adding tuples with pair.second >= comparison to the result
std::vector<std::pair<unsigned, double>> GE(const std::vector<std::pair<unsigned, double>>& input, double comparison,
                                            double selectivityEstimate) {
   std::vector<std::pair<unsigned, double>> result;
   result.reserve(input.size() * selectivityEstimate);
   for (const auto& p : input)
      if (p.second >= comparison)
         result.push_back(p);
   return result;
}

// Sum aggregation, unconditionally adding up all pair.second values
double sum(const std::vector<std::pair<unsigned, double>>& input) {
   double result = 0;
   for (const auto& p : input)
      result += p.second;
   return result;
}

//------------------------------------------------------------------------
// CedarDB Style method

// A single method performing all steps of a query
double processQuery(const std::vector<std::pair<unsigned, double>>& input,
                    const std::unordered_set<unsigned>& joinTable,
                    double comparison) {
   double result = 0;
   for (const auto& p : input)
      if (p.second >= comparison && joinTable.contains(p.first))
         result += p.second;
   return result;
}

void queryBenchmark() {
   // Fix the benchmark parameters
   auto inputSize = 1'000'000'000u;
   auto numWarmups = 3u;

   // Prepare space for the input data
   std::vector<std::pair<unsigned, double>> input;
   input.reserve(inputSize);

   // Select double comparison value for GE comparison
   double compValue = -4.0;

   // Prepare join hash table
   std::unordered_set<unsigned> joinTable;
   joinTable.reserve(inputSize * .01);

   // Prepare random number generation to fill input
   std::mt19937 gen(42);
   std::uniform_real_distribution<double> uniform_dist(-5.0, 6.0);
   std::uniform_int_distribution<int> selection_dist(1, 100);

   // Prepare result variable
   double result;

   for (auto i = 0; i < inputSize; i++) {
      // Fill input with ascending keys and random values
      input.push_back({i, uniform_dist(gen)});

      // Select every 100th tuple for the join
      if (selection_dist(gen) > 99) {
         joinTable.insert(i);
      }
   }

   auto measureDuckDB = [&]() {
      auto res = GE(input, compValue, .85);
      res = semiJoin(res, joinTable, .01);
      result = sum(res);
   };

   auto measureCedarDB = [&]() {
      result = processQuery(input, joinTable, compValue);
   };

   // Benchmark DuckDB style
   for (auto i = 0; i < numWarmups; i++) {
      measureDuckDB();
   }
   {
      std::cout << "DuckDB Style:" << std::endl;
      PerfEventBlock e(inputSize / 100);
      measureDuckDB();
      e.e->stopCounters();
      std::cout << "Result: " << result << std::endl;
      std::cout << "Stats: " << std::endl;
   }
   std::cout << std::endl
             << std::endl
             << std::endl;

   // Benchmark CedarDB style
   for (auto i = 0; i < numWarmups; i++) {
      measureCedarDB();
   }
   {
      std::cout << "CedarDB Style:" << std::endl;
      PerfEventBlock e(inputSize / 100);
      measureCedarDB();
      e.e->stopCounters();
      std::cout << "Result: " << result << std::endl;
      std::cout << "Stats: " << std::endl;
   }
   std::cout << std::endl
             << std::endl
             << std::endl;
}

//------------------------------------------------------------------------
// Benchmark

int main() {
   std::cout << "Microbenchmark: " << std::endl
             << std::endl;
   microBenchmark();

   std::cout << std::endl
             << std::endl
             << std::endl
             << "Query Benchmark: " << std::endl
             << std::endl;
   queryBenchmark();

   return 0;
}
