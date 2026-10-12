# Dapr XKCD

An [XKCD](https://xkcd.com) reader proxy. Runs scheduled jobs to request the latest comic, as well as incrementing through each comic and saving it to a local Postgres database. This prevents blasting the real XKCD API.

Build with a set of [Dapr components](https://dapr.io).

## Requirements

- Docker
- Dapr CLI
- (optional) Mise
  - Can install all requirements at once with `mise install`
- `direnv`

## Setup

### Development

- Copy `.example.env` to `.env` and edit it
  - Set a Postgres password
  - Optionally change port bindings for services
- Run `direnv allow`
- Bring up the development containers with `docker compose -f .containers/dev.compose.yml up -d`
- Run the Go API with [`./scripts/start-dev-api.sh`](./scripts/start-dev-api.sh)
- Navigate to `http://127.0.0.1:8000/`

### Production

- - Copy `.example.env` to `.env` and edit it
  - Set a Postgres password
  - Optionally change port bindings for services
- Run `direnv allow`
- Bring the stack up with `docker compose up -d --build`
- Navigate to `http://127.0.0.1/`
