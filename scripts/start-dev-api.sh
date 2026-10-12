#!/usr/bin/env bash
set -euo pipefail

SCRIPT_NAME="$(basename -- "${BASH_SOURCE[0]}")"

usage() {
  cat <<EOF
Usage:
  ${SCRIPT_NAME} <app-id>
  ${SCRIPT_NAME} -h | --help

Stop a local Dapr instance by application ID.

Arguments:
  app-id      Application ID of the Dapr instance to stop.

Options:
  -h, --help  Show this help menu and exit.

Examples:
  ${SCRIPT_NAME} xkcd-archive
  ${SCRIPT_NAME} --help
EOF
}

if [[ $# -eq 1 && ("$1" == "-h" || "$1" == "--help") ]]; then
  usage
  exit 0
fi

if [[ $# -ne 1 ]]; then
  printf 'Error: exactly one application ID is required.\n\n' >&2
  usage >&2
  exit 2
fi

APP_ID="$1"

if [[ "$APP_ID" =~ ^[[:space:]]*$ ]]; then
  printf 'Error: application ID must not be empty or whitespace-only.\n' >&2
  exit 2
fi

if [[ "$APP_ID" == -* ]]; then
  printf 'Error: unknown option: %s\n\n' "$APP_ID" >&2
  usage >&2
  exit 2
fi

if ! command -v dapr >/dev/null 2>&1; then
  printf 'Error: dapr CLI is not installed or is not in PATH.\n' >&2
  exit 1
fi

printf 'Stopping Dapr instance: %s\n' "$APP_ID"

exec dapr stop --app-id "$APP_ID"
