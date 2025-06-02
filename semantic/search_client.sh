#!/bin/bash

. ./env.sh

max_results="${MAX_RESULTS:-5}"

os=$( uname -o )
base64="base64"
if [[ $os == *"Linux"* ]]
then
  base64="$base64 -w 0"
fi

if [ $# -lt 1 ]
then
  echo "Usage: $0 word [word2 ... wordN]"
  exit 1
fi

curl -s http://$FLASK_HOST:$FLASK_PORT/search/$( echo -n "$@" | $base64 )/$max_results | jq

