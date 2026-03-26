#!/bin/bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
OUTPUT_DIR=${1:-"$SCRIPT_DIR/../data"}

calculate_hash() {
    sha256sum "$1" | awk '{print $1}'
}

GZ_FILE_PATH="$OUTPUT_DIR/01302020.NASDAQ_ITCH50.gz"
GZ_EXPECTED_HASH="8f5399155242f8d84338c36fdd78a47c0b0aa3df5707829b20c5158c258364c6"
UNZIPPED_FILE_PATH="$OUTPUT_DIR/01302020.NASDAQ_ITCH50"
UNZIPPED_EXPECTED_HASH="a324b2cc6daa0992e411d021b1f714cbc614dc5d1d3f2828b19b79d3f9fbe75b"
DOWNLOAD_URL="https://emi.nasdaq.com/ITCH/Nasdaq%20ITCH/01302020.NASDAQ_ITCH50.gz"

mkdir -p "$OUTPUT_DIR"

if [[ -f "$UNZIPPED_FILE_PATH" && $(calculate_hash "$UNZIPPED_FILE_PATH") == "$UNZIPPED_EXPECTED_HASH" ]]; then
    echo "Found extracted NASDAQ dump. Skipping download and extraction."
else
    if [[ -f "$GZ_FILE_PATH" && $(calculate_hash "$GZ_FILE_PATH") == "$GZ_EXPECTED_HASH" ]]; then
        echo "Found compressed NASDAQ dump. Skipping download."
    else
        echo "Downloading NASDAQ dump..."
        wget -nv -O "$GZ_FILE_PATH" "$DOWNLOAD_URL"
    fi
    printf "Unzipping ...\n"
    gunzip -f "$GZ_FILE_PATH"
fi

printf "Unzipping complete, parsing messages...\n"
python3 "$SCRIPT_DIR/parser.py" "$UNZIPPED_FILE_PATH" "$OUTPUT_DIR"
