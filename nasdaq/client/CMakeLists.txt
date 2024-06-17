cmake_minimum_required(VERSION 3.27)
project(NasdaqDriver)

set(CMAKE_CXX_STANDARD 23)

find_package(PostgreSQL 15 REQUIRED)


add_executable(NasdaqDriver main.cpp
        NasdaqClient.cpp
        NasdaqClient.h)
target_link_libraries(NasdaqDriver PostgreSQL::PostgreSQL)