#!/bin/bash

. ./docker_include.sh

# Ref: https://everythingdevops.dev/building-x86-images-on-an-apple-m1-chip/
if [[ "x86" == "$arch" ]]
then
  docker buildx build --platform=linux/amd64 -t $docker_id/$img_name .
elif [[ "arm" == "$arch" ]]
then
  docker build -t $docker_id/$img_name .
else
  echo "Unknown arch '$arch'" 
  exit 1
fi

