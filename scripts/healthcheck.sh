#!/usr/bin/env bash
set -euo pipefail

SCRIPT_NAME="$(basename -- "${BASH_SOURCE[0]}")"

usage() {
  cat <<EOF
Usage:
  ${SCRIPT_NAME} [base-url]
  ${SCRIPT_NAME} -h | --help

Check the API's /health/live endpoint.

Arguments:
  base-url    API address, including http:// or https://.
              Example: http://127.0.0.1:8281

Environment:
  API_BASE_URL         Default API address.
                       Falls back to http://127.0.0.1:\${APP_PORT:-8281}
  HEALTHCHECK_TIMEOUT  Maximum request duration in seconds.
                       Default: 10

Examples:
  ${SCRIPT_NAME}
  ${SCRIPT_NAME} http://colossus:8281
  API_BASE_URL=https://comics.example.com ${SCRIPT_NAME}
EOF
}

if [[ $# -eq 1 && ("$1" == "-h" || "$1" == "--help") ]]; then
  usage
  exit 0
fi

if [[ $# -gt 1 ]]; then
  printf 'Error: expected at most one base URL.\n\n' >&2
  usage >&2
  exit 2
fi

BASE_URL="${1:-${API_BASE_URL:-http://127.0.0.1:${APP_PORT:-8281}}}"
TIMEOUT="${HEALTHCHECK_TIMEOUT:-10}"

case "$BASE_URL" in
http://?* | https://?*) ;;
*)
  printf 'Error: base URL must start with http:// or https://.\n' >&2
  exit 2
  ;;
esac

if [[ ! "$TIMEOUT" =~ ^[1-9][0-9]*$ ]]; then
  printf 'Error: HEALTHCHECK_TIMEOUT must be a positive integer.\n' >&2
  exit 2
fi

if ! command -v curl >/dev/null 2>&1; then
  printf 'Error: curl is not installed or is not in PATH.\n' >&2
  exit 1
fi

URL="${BASE_URL%/}/health/live"

printf 'Checking API liveness: %s\n' "$URL"

if STATUS="$(
  curl \
    --silent \
    --show-error \
    --connect-timeout 5 \
    --max-time "$TIMEOUT" \
    --output /dev/null \
    --write-out '%{http_code}' \
    "$URL"
)"; then
  if [[ "$STATUS" == "204" ]]; then
    printf 'OK: API is responding (HTTP %s).\n' "$STATUS"
    exit 0
  fi

  printf 'Error: expected HTTP 204, received HTTP %s.\n' "$STATUS" >&2
  exit 1
else
  RESULT=$?
  printf 'Error: healthcheck request failed (curl exit %s).\n' "$RESULT" >&2
  exit "$RESULT"
fi
