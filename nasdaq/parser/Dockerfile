FROM python:3.14-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates wget gzip \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY parser.py prepare.sh /app/

RUN chmod +x /app/prepare.sh

CMD ["/app/prepare.sh", "./data"]
