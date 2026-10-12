#!/usr/bin/env bash
set -euo pipefail

THIS_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
PROJECT_ROOT=$(realpath -m "${THIS_DIR}/..")
CWD=$(pwd)
trap 'cd "${CWD}"' EXIT

cd "${PROJECT_ROOT}"

docker compose exec postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c '
    SELECT
      c.id,
      c.title,
      i.content_type,
      i.byte_size,
      f.status,
      f.attempts
    FROM comics c
    JOIN comic_images i ON i.comic_id = c.id
    JOIN comic_fetches f ON f.comic_id = c.id
    ORDER BY c.id DESC
    LIMIT 10;
  '
