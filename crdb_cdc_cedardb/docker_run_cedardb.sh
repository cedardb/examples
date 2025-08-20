#!/bin/bash

if [ $# -ne 1 ]
then
  echo "Usage: $0 CedardDB_Image_Name"
  exit 1
fi

data_dir="$HOME/CedarDB/data"

# -e VERBOSITY=DEBUG1 \

docker run -d --rm -p 5432:5432 \
  -e VERBOSITY=DEBUG1 \
  -v $data_dir:/var/lib/cedardb/data \
  -e CEDAR_PASSWORD=postgres \
  --name cedardb $1

