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

# URL-safe Base64 encoding
url_safe_b64()
{
  echo -n "$@" | $base64 | sed 's/+/-/g' | sed 's/\//_/g'
}

query_b64=$( url_safe_b64 "$@" )
if [[ -z "$constraint" ]]
then
  curl -s http://$FLASK_HOST:$FLASK_PORT/search/$query_b64/$max_results | jq
else
  constraint_b64=$( url_safe_b64 $constraint )
  curl -s http://$FLASK_HOST:$FLASK_PORT/search/$query_b64/$max_results/$constraint_b64 | jq
fi

