#!/bin/bash

echo "Stopping CockroachDB container ..."
id=$( docker ps -q --filter "ancestor=cockroachdb/cockroach:latest" )
if [ -n "$id" ] ; then docker stop $id && docker rm $id ; fi

echo "Stopping CedarDB container ..."
id=$( docker ps -q --filter "ancestor=cedardb/cedardb" )
if [ -n "$id" ] ; then docker stop $id && docker rm $id ; fi

echo "Stopping webhook container ..."
id=$( docker ps -q --filter "ancestor=crdb-cdc-webhook" )
if [ -n "$id" ] ; then docker stop $id ; fi

