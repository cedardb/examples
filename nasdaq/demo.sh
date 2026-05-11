#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
    cat <<'EOF'
Usage: 
    ./demo.sh <start|stop|clean|pull>
or  DB_CPU_LIMIT=X DB_MEM_LIMIT=XXgb ./demo.sh [--comparison|-c] <start|stop|clean|pull>

Normal mode starts CedarDB, Nasdaq client, Grafana and the AI chat for the demo.
Comparison mode adds PostgreSQL to the deployment to compare the performance of the two DBs. 

Commands:
  start   Start the demo stack
  stop    Stop the demo stack
  clean   Stop the demo stack and remove volumes
  pull    Pull the latest images for the demo stack

Options:
  -c, --comparison   Use comparison.compose.yml; requires DB_CPU_LIMIT and DB_MEM_LIMIT
  -h, --help         Show this help message
EOF
}

comparison=false
command=""

while (($# > 0)); do
    case "$1" in
        -c|--comparison)
            comparison=true
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        start|stop|clean|pull)
            if [[ -n "$command" ]]; then
                echo "Only one command is allowed: start, stop, clean, or pull." >&2
                usage >&2
                exit 1
            fi
            command="$1"
            ;;
        *)
            echo "Unknown argument: $1" >&2
            usage >&2
            exit 1
            ;;
    esac
    shift
done

if [[ -z "$command" ]]; then
    echo "Missing command: start, stop, clean, or pull." >&2
    usage >&2
    exit 1
fi

compose_args=()
if $comparison; then
    compose_args=(-f comparison.compose.yml)
fi

if $comparison && [[ -z "${DB_CPU_LIMIT:-}" || -z "${DB_MEM_LIMIT:-}" ]]; then
    echo $'Comparison mode requires DB_CPU_LIMIT and DB_MEM_LIMIT to be set.\n' >&2
    usage >&2
    exit 1
fi

export GRAFANA_USER="${GRAFANA_USER:-grafana}"
export GRAFANA_USER_PWD="${GRAFANA_USER_PWD:-supersafepassword}"
export ADMIN_PWD="${ADMIN_PWD:-evensaferpassword}"

case "$command" in
    start)
        if [[ ! -s "db-config/cedar/license.env" ]]; then
            printf '%s\n' \
                "Cedar license missing at db-config/cedar/license.env, falling back to admin user for grafana!" \
                "License is required for granting permissions to users." \
                "Get your trial license at: console.cedardb.com" \
                "" >&2

            export GRAFANA_USER="postgres"
            export GRAFANA_USER_PWD="${ADMIN_PWD}"
        fi
        docker compose "${compose_args[@]}" up -d --build --force-recreate --remove-orphans
        ;;
    stop)
        docker compose "${compose_args[@]}" down
        ;;
    clean)
        docker compose "${compose_args[@]}" down -v
        ;;
    pull)
        docker compose "${compose_args[@]}" pull --ignore-buildable
        ;;
esac
