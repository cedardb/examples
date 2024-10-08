#!/bin/bash

# Prepare Build
c++ -std=c++20 -O3 -g -Wall -Werror -march=native -fno-tree-vectorize -o data_layout_no_vectorization main.cpp
c++ -std=c++20 -O3 -g -Wall -Werror -march=native -o data_layout main.cpp

clear
echo "Performing Benchmark"

echo "Without Auto Vectorization (-fno-tree-vectorize)"
./data_layout_no_vectorization

echo "------------------------------------------------"
echo "With Auto Vectorization"
./data_layout

