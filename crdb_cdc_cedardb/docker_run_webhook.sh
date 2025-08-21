#!/bin/bash

# This is the CedarDB connection string corresponding to its Docker startup command:
#export PG_DSN="postgresql://postgres:postgres@localhost:5432/postgres"
export PG_DSN="postgresql://postgres:postgres@host.docker.internal:5432/postgres"

# Generate SSL cert
rm -f key.pem cert.pem
yes | openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -nodes \
  -subj '/CN=localhost' \
  -addext 'subjectAltName = DNS:localhost.localdomain, DNS:host.docker.internal' \
  -extensions SAN \
  -config <(cat /etc/ssl/openssl.cnf \
            <(printf "[SAN]\nsubjectAltName='DNS:localhost'"))

export TLS_KEY=$( cat key.pem )
export TLS_CERT=$( cat cert.pem )

. ./docker_include.sh

docker run -d --rm -p 8443:8443 \
  -e PG_DSN -e TLS_KEY -e TLS_CERT \
  --name cdc-webhook "$docker_id/$img_name"

