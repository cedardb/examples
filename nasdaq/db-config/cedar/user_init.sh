#!/usr/bin/env bash
set -Eeuo pipefail

echo "CREATE USER grafana WITH PASSWORD '$GRAFANA_USER_PWD';" | process_sql
