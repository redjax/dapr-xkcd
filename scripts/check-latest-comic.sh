#!/usr/bin/env bash
set -euo pipefail

SCRIPT_NAME="$(basename -- "${BASH_SOURCE[0]}")"

usage() {
  cat <<EOF
Usage:
  ${SCRIPT_NAME} [base-url]
  ${SCRIPT_NAME} -h | --help

Trigger a latest-comic check by posting to the API's /latest endpoint.

Arguments:
  base-url    API address, including http:// or https://.
              Example: http://127.0.0.1:8281

Environment:
  API_BASE_URL          Default API address.
                        Falls back to http://127.0.0.1:\${APP_PORT:-8281}
  LATEST_CHECK_TIMEOUT  Maximum request duration in seconds.
                        Default: 150

Examples:
  ${SCRIPT_NAME}
  ${SCRIPT_NAME} http://colossus:8281
  API_BASE_URL=https://comics.example.com ${SCRIPT_NAME}
  LATEST_CHECK_TIMEOUT=180 ${SCRIPT_NAME}
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
TIMEOUT="${LATEST_CHECK_TIMEOUT:-150}"

case "$BASE_URL" in
http://?* | https://?*) ;;
*)
  printf 'Error: base URL must start with http:// or https://.\n' >&2
  exit 2
  ;;
esac

if [[ ! "$TIMEOUT" =~ ^[1-9][0-9]*$ ]]; then
  printf 'Error: LATEST_CHECK_TIMEOUT must be a positive integer.\n' >&2
  exit 2
fi

if ! command -v curl >/dev/null 2>&1; then
  printf 'Error: curl is not installed or is not in PATH.\n' >&2
  exit 1
fi

BODY_FILE="$(mktemp)"
trap 'rm -f -- "$BODY_FILE"' EXIT

URL="${BASE_URL%/}/latest"

printf 'Requesting latest-comic check: %s\n' "$URL"

if STATUS="$(
  curl \
    --silent \
    --show-error \
    --request POST \
    --connect-timeout 5 \
    --max-time "$TIMEOUT" \
    --output "$BODY_FILE" \
    --write-out '%{http_code}' \
    "$URL"
)"; then
  if [[ "$STATUS" == "204" ]]; then
    printf 'OK: request acknowledged (HTTP %s).\n' "$STATUS"
    printf 'The current API also returns 204 when another ingestion job is already running.\n'
    exit 0
  fi

  printf 'Error: expected HTTP 204, received HTTP %s.\n' "$STATUS" >&2

  if [[ -s "$BODY_FILE" ]]; then
    printf 'Response body:\n' >&2
    cat "$BODY_FILE" >&2
    printf '\n' >&2
  fi

  exit 1
else
  RESULT=$?
  printf 'Error: latest-comic request failed (curl exit %s).\n' "$RESULT" >&2
  exit "$RESULT"
fi
