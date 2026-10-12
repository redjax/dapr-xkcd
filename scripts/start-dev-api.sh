#!/usr/bin/env bash
set -euo pipefail

THIS_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
PROJECT_ROOT="$(cd -- "${THIS_DIR}/.." && pwd -P)"

cd "${PROJECT_ROOT}"

: "${APP_PORT:?Load the project environment with direnv first}"
: "${DAPR_HTTP_PORT:?Set DAPR_HTTP_PORT}"
: "${DAPR_GRPC_PORT:?Set DAPR_GRPC_PORT}"

: "${POSTGRES_HOST:?Set POSTGRES_HOST}"
: "${POSTGRES_PORT:?Set POSTGRES_PORT}"
: "${POSTGRES_DB:?Set POSTGRES_DB}"
: "${POSTGRES_USER:?Set POSTGRES_USER}"
: "${POSTGRES_PASSWORD:?Set POSTGRES_PASSWORD}"

: "${XKCD_BASE_URL:?Set XKCD_BASE_URL}"
: "${XKCD_MAX_RESPONSE_BODY_SIZE:?Set XKCD_MAX_RESPONSE_BODY_SIZE}"
: "${LATEST_SCHEDULE:?Set LATEST_SCHEDULE}"
: "${BACKFILL_SCHEDULE:?Set BACKFILL_SCHEDULE}"

mkdir -p bin
go build -o bin/xkcd-api ./cmd/api

exec dapr run \
  --app-id xkcd-archive \
  --app-port "$APP_PORT" \
  --app-channel-address 127.0.0.1 \
  --dapr-http-port "$DAPR_HTTP_PORT" \
  --dapr-grpc-port "$DAPR_GRPC_PORT" \
  --placement-host-address " " \
  --scheduler-host-address " " \
  --resources-path ./dapr/components \
  -- ./bin/xkcd-api
