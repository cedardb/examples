cmake_minimum_required(VERSION 3.27)
project(NasdaqLoaderCpp)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

add_executable(parser parser.cpp)
