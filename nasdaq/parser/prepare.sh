#!/bin/bash
set -euo pipefail

calculate_hash() {
    sha256sum "$1" | awk '{print $1}'
}

GZ_FILE_PATH="data/01302020.NASDAQ_ITCH50.gz"
GZ_EXPECTED_HASH="8f5399155242f8d84338c36fdd78a47c0b0aa3df5707829b20c5158c258364c6"
UNZIPPED_FILE_PATH="data/01302020.NASDAQ_ITCH50"
UNZIPPED_EXPECTED_HASH=a324b2cc6daa0992e411d021b1f714cbc614dc5d1d3f2828b19b79d3f9fbe75b
DOWNLOAD_URL="https://emi.nasdaq.com/ITCH/Nasdaq%20ITCH/01302020.NASDAQ_ITCH50.gz"


if [[ -f "$UNZIPPED_FILE_PATH" && $(calculate_hash "$UNZIPPED_FILE_PATH") == "$UNZIPPED_EXPECTED_HASH" ]]; then
    echo "Found extracted NASDAQ dump. Skipping download and extraction."
else
    if [[ -f "$GZ_FILE_PATH" && $(calculate_hash "$GZ_FILE_PATH") == "$GZ_EXPECTED_HASH" ]]; then
        echo "Found compressed NASDAQ dump. Skipping download ..."
    else
        echo "Downloading NASDAQ dump..."
        mkdir -p data 
        wget -nv -P data "$DOWNLOAD_URL"
    fi
    printf "Unzipping ...\n"
    gunzip "$GZ_FILE_PATH"
fi

printf "Unzipping complete, checking parsed CSV files...\n"

ALL_HASHES_MATCH=$([[ -f "data/cancellations.csv" && $(calculate_hash "data/cancellations.csv") == "9f22c0b4769a449753a2a4ddfb735a2d72c9393c47dc139546e4bc3be5350d2e" ]] && echo true || echo false)
ALL_HASHES_MATCH=$([[ "$ALL_HASHES_MATCH" == "true" && -f "data/cancellationsPreMarket.csv" && $(calculate_hash "data/cancellationsPreMarket.csv") == "0c8f8ee0a408e568ba581f5e71ffd2e9b4cfbfa0b448d0d737d5e042c66fee2f" ]] && echo true || echo false)
ALL_HASHES_MATCH=$([[ "$ALL_HASHES_MATCH" == "true" && -f "data/executions.csv" && $(calculate_hash "data/executions.csv") == "0d9137de87c348e803189c7495fff03296ddbfafa3cfd8ec5a4a91ec96db275c" ]] && echo true || echo false)
ALL_HASHES_MATCH=$([[ "$ALL_HASHES_MATCH" == "true" && -f "data/executionsPreMarket.csv" && $(calculate_hash "data/executionsPreMarket.csv") == "b36f6879b29c778ebb90f7688e5f07aaccc13cf9831889bc5ec5c6a426b819ae" ]] && echo true || echo false)
ALL_HASHES_MATCH=$([[ "$ALL_HASHES_MATCH" == "true" && -f "data/marketMakers.csv" && $(calculate_hash "data/marketMakers.csv") == "ee85cd89895f8d8cf90e47666f7272f39b73b23cc0461dcff64fde1c71a1aa75" ]] && echo true || echo false)
ALL_HASHES_MATCH=$([[ "$ALL_HASHES_MATCH" == "true" && -f "data/orders.csv" && $(calculate_hash "data/orders.csv") == "b311010525333f6023f7024a43ad52eb3c482a2700eb34a82fa979443bb180bc" ]] && echo true || echo false)
ALL_HASHES_MATCH=$([[ "$ALL_HASHES_MATCH" == "true" && -f "data/ordersPreMarket.csv" && $(calculate_hash "data/ordersPreMarket.csv") == "ae603a0cba3dbb9522808007b5d6470b168d999a429b4fb609366487bb36f219" ]] && echo true || echo false)
ALL_HASHES_MATCH=$([[ "$ALL_HASHES_MATCH" == "true" && -f "data/stocks.csv" && $(calculate_hash "data/stocks.csv") == "7d6e41399afc03559eeffe548c788e8cc903af09cee8aec709c9da3ade3f2e3a" ]] && echo true || echo false)

if [[ "$ALL_HASHES_MATCH" == "true" ]]; then
    echo "Found parsed CSV files with matching hashes. Skipping parsing."
else
    printf "Parsing messages...\n"
    python3 parser.py "$UNZIPPED_FILE_PATH" "data/"
fi
