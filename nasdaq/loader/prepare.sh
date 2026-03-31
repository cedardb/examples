#!/bin/bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
OUTPUT_DIR=${1:-"$SCRIPT_DIR/../data"}

hash_matches() {
    local file_path=$1
    local expected_hash=$2

    [[ -f "$file_path" && $(sha256sum "$file_path" | awk '{print $1}') == "$expected_hash" ]]
}

GZ_FILE_PATH="$OUTPUT_DIR/01302020.NASDAQ_ITCH50.gz"
GZ_EXPECTED_HASH="8f5399155242f8d84338c36fdd78a47c0b0aa3df5707829b20c5158c258364c6"
UNZIPPED_FILE_PATH="$OUTPUT_DIR/01302020.NASDAQ_ITCH50"
UNZIPPED_EXPECTED_HASH="a324b2cc6daa0992e411d021b1f714cbc614dc5d1d3f2828b19b79d3f9fbe75b"
DOWNLOAD_URL="https://emi.nasdaq.com/ITCH/Nasdaq%20ITCH/01302020.NASDAQ_ITCH50.gz"

mkdir -p "$OUTPUT_DIR"

if hash_matches "$UNZIPPED_FILE_PATH" "$UNZIPPED_EXPECTED_HASH"; then
    echo "Found extracted NASDAQ dump. Skipping download and extraction."
else
    if hash_matches "$GZ_FILE_PATH" "$GZ_EXPECTED_HASH"; then
        echo "Found compressed NASDAQ dump. Skipping download."
    else
        echo "Downloading NASDAQ dump..."
        wget -nv -O "$GZ_FILE_PATH" "$DOWNLOAD_URL"
    fi
    printf "Unzipping ...\n"
    gunzip -f "$GZ_FILE_PATH"
fi

CANCELLATIONS_HASH="9f22c0b4769a449753a2a4ddfb735a2d72c9393c47dc139546e4bc3be5350d2e"
CANCELLATIONS_PATH="$OUTPUT_DIR/cancellations.csv"
CANCELLATIONSPREMARKET_HASH="0c8f8ee0a408e568ba581f5e71ffd2e9b4cfbfa0b448d0d737d5e042c66fee2f"
CANCELLATIONSPREMARKET_PATH="$OUTPUT_DIR/cancellationsPreMarket.csv"
EXECUTIONS_HASH="0d9137de87c348e803189c7495fff03296ddbfafa3cfd8ec5a4a91ec96db275c"
EXECUTIONS_PATH="$OUTPUT_DIR/executions.csv"
EXECUTIONSPREMARKET_HASH="b36f6879b29c778ebb90f7688e5f07aaccc13cf9831889bc5ec5c6a426b819ae"
EXECUTIONSPREMARKET_PATH="$OUTPUT_DIR/executionsPreMarket.csv"
MARKETMAKERS_HASH="ee85cd89895f8d8cf90e47666f7272f39b73b23cc0461dcff64fde1c71a1aa75"
MARKETMAKERS_PATH="$OUTPUT_DIR/marketMakers.csv"
ORDERS_HASH="b311010525333f6023f7024a43ad52eb3c482a2700eb34a82fa979443bb180bc"
ORDERS_PATH="$OUTPUT_DIR/orders.csv"
ORDERSPREMARKET_HASH="ae603a0cba3dbb9522808007b5d6470b168d999a429b4fb609366487bb36f219"
ORDERSPREMARKET_PATH="$OUTPUT_DIR/ordersPreMarket.csv"
STOCKS_HASH="7d6e41399afc03559eeffe548c788e8cc903af09cee8aec709c9da3ade3f2e3a"
STOCKS_PATH="$OUTPUT_DIR/stocks.csv"

if hash_matches "$CANCELLATIONS_PATH" "$CANCELLATIONS_HASH" \
    && hash_matches "$CANCELLATIONSPREMARKET_PATH" "$CANCELLATIONSPREMARKET_HASH" \
    && hash_matches "$EXECUTIONS_PATH" "$EXECUTIONS_HASH" \
    && hash_matches "$EXECUTIONSPREMARKET_PATH" "$EXECUTIONSPREMARKET_HASH" \
    && hash_matches "$MARKETMAKERS_PATH" "$MARKETMAKERS_HASH" \
    && hash_matches "$ORDERS_PATH" "$ORDERS_HASH" \
    && hash_matches "$ORDERSPREMARKET_PATH" "$ORDERSPREMARKET_HASH" \
    && hash_matches "$STOCKS_PATH" "$STOCKS_HASH"; then
    echo "Parsed CSV files match expected hashes. Skipping parser."
else
    printf "Parsing messages...\n"
    python3 "$SCRIPT_DIR/parser.py" "$UNZIPPED_FILE_PATH" "$OUTPUT_DIR"
fi
