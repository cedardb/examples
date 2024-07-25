#!/bin/bash

# Prepare Build
echo "Building..."
mkdir -p build
cd build
cmake -DCMAKE_BUILD_TYPE=Release ..
cmake --build . --target memory_movement

clear
echo "Performing Benchmark"

# Check for perf settings

# Get current perf setting
perfSetting=$(cat /proc/sys/kernel/perf_event_paranoid)


if [ "$perfSetting" -gt "1" ]; then
	echo "Insufficient Priviliges: Cannot access perf counters"
	echo "Please run"
	echo "sudo sh -c 'echo 1 > /proc/sys/kernel/perf_event_paranoid'"
	exit -1
fi
./memory_movement
