# Data Movement Benchmarks
This repository contains all code for the [The Hidden Cost Of Data Movement](https://cedardb.com/blog/reducing_data_movement) blogpost.

The benchmarks will require roughly 40 GB of memory to execute correctly. To run it on a smaller system, consider reducing the `inputSize` variables in lines 36 and 143 of `main.cpp` accordingly.

To build and execute all benchmarks, simply run

```
sh runBenchmark.sh
```

This benchmark depends on perf to run. 
In case of too low perf_event priviliges, the script will give instructions on how to increase them.
