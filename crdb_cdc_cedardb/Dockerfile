#
# docker run -e PG_DSN=$PG_DSN -e TLS_CERT="/certs/cert.pem" -e TLS_KEY="/certs/key.pem" 27984446a1b2
#

# build stage
FROM golang:1.23-alpine AS builder

# If you need to pull modules over HTTPS/Git
RUN apk add --no-cache git

WORKDIR /app

# Cache modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source & build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o crdb_cdc_cedardb .

# runtime stage
FROM alpine:3.18 AS runtime

# Install openssl for cert generation, plus CA certs for any HTTPS clients
RUN apk add --no-cache \
      ca-certificates \
      openssl \
 && update-ca-certificates

# Generate a self-signed cert & key (valid 365d) at build time
# Adjust -subj fields as needed, or remove this block if you'll mount your own certs
RUN mkdir -p /certs \
 && openssl req -x509 -nodes -days 365 \
      -newkey rsa:2048 \
      -subj "/C=US/ST=State/L=City/O=Org/CN=localhost" \
      -addext "subjectAltName = DNS:localhost.localdomain, DNS:host.docker.internal" \
      -keyout /certs/key.pem \
      -out /certs/cert.pem

# Copy the Go binary
COPY --from=builder /app/crdb_cdc_cedardb /usr/local/bin/crdb_cdc_cedardb

# Expose both your app port and HTTPS port if different
EXPOSE 8443

# ENTRYPOINT; your app can read certs from /certs/cert.pem & /certs/key.pem
ENTRYPOINT ["/usr/local/bin/crdb_cdc_cedardb"]

