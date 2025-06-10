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
  echo "Usage: $0 [-c URL_constraint] word [word2 ... wordN]" >&2
  exit 1
fi

constraint=""
while getopts ":c:" opt
do
  case $opt in
    c)
      constraint="$OPTARG"
      ;;
    \?)
      echo "Error: invalid option -$OPTARG" >&2
      exit 1
      ;;
    :)
      echo "Error: option -$OPTARG requires an argument." >&2
      exit 1
      ;;
  esac
done
# remove the options parsed from $@
shift $((OPTIND - 1))

if [[ -z "$constraint" ]]
then
  curl -s http://$FLASK_HOST:$FLASK_PORT/search/$( echo -n "$@" | $base64 )/$max_results | jq
else
  curl -s http://$FLASK_HOST:$FLASK_PORT/search/$( echo -n "$@" | $base64 )/$max_results/$constraint | jq
fi

