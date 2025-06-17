#!/bin/bash

cat <<EoM

CREATE CHANGEFEED FOR TABLE public.osm_names
INTO 'webhook-https://mikemac.local:8443/cdc/geohash3,city,name?insecure_tls_skip_verify=true'
WITH updated;

EoM

# https://serverfault.com/questions/73689/how-to-create-a-multi-domain-self-signed-certificate-for-apache2
rm -f key.pem cert.pem
yes | openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -nodes \
  -subj '/CN=localhost' \
  -addext 'subjectAltName = DNS:localhost.localdomain, DNS:mikemac.local' \
  -extensions SAN \
  -config <(cat /etc/ssl/openssl.cnf \
            <(printf "[SAN]\nsubjectAltName='DNS:localhost'"))

#export PG_DSN="postgresql://postgres:postgres@localhost:5432/postgres"
export PG_DSN="postgresql://postgres:postgres@localhost:5432/movr"
export TLS_KEY="key.pem"
export TLS_CERT="cert.pem"

EXE="./crdb_cdc_cedardb"

if [[ ! -x $EXE ]]
then
  go build .
fi

exec $EXE

