FROM ubuntu:24.04 AS build

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates cmake g++ make \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY CMakeLists.txt parser.cpp /src/

RUN cmake -S /src -B /build -DCMAKE_BUILD_TYPE=Release \
    && cmake --build /build --config Release

FROM ubuntu:24.04

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates wget gzip \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=build /build/parser /app/parser
COPY prepare.sh /app/

RUN chmod +x /app/prepare.sh

CMD ["/app/prepare.sh", "./data"]
