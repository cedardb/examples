#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
    cat <<'EOF'
Usage: ./demo.sh [--comparison|-c] <start|stop|clean>

Commands:
  start   Start the demo stack
  stop    Stop the demo stack
  clean   Stop the demo stack and remove volumes

Options:
  -c, --comparison   Use comparison.compose.yml
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
        start|stop|clean)
            if [[ -n "$command" ]]; then
                echo "Only one command is allowed: start, stop, or clean." >&2
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
    echo "Missing command: start, stop, or clean." >&2
    usage >&2
    exit 1
fi

compose_args=()
if $comparison; then
    compose_args=(-f comparison.compose.yml)
fi

export GRAFANA_USER="${GRAFANA_USER:-grafana}"
export GRAFANA_USER_PWD="${GRAFANA_USER_PWD:-supersafepassword}"
export ADMIN_PWD="${ADMIN_PWD:-evensaferpassword}"

case "$command" in
    start)
        if [[ ! -s "db-config/cedar/license.env" ]]; then
            echo "Cedar license missing at db-config/cedar/license.env, falling back to admin user for grafana!" >&2
            echo "License is required for granting permissions to users." >&2
            echo "Get your trial license at: console.cedardb.com" >&2
            echo "" >&2

            export GRAFANA_USER="postgres"
            export GRAFANA_USER_PWD="${ADMIN_PWD}"
        fi
        docker compose "${compose_args[@]}" up -d --build --force-recreate
        ;;
    stop)
        docker compose "${compose_args[@]}" down
        ;;
    clean)
        docker compose "${compose_args[@]}" down -v
        ;;
esac
