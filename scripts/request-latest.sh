#!/usr/bin/env bash
set -euo pipefail

curl --fail-with-body -sS "http://127.0.0.1:${APP_PORT}/comics/latest" | jq
