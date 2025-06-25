#!/bin/bash

. ./env.sh

os=$( uname -o )
base64="base64"
if [[ $os == *"Linux"* ]]
then
  base64="$base64 -w 0"
fi

if [ $# -ne 1 ]
then
  echo "Usage: $0 URL_to_index"
  exit 1
fi

status=$( curl -s http://$FLASK_HOST:$FLASK_PORT/index/$( echo -n "$1" | base64 ) )
echo "$1 => $status"

