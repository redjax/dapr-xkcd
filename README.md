# Dapr XKCD

An [XKCD](https://xkcd.com) reader proxy. Runs scheduled jobs to request the latest comic, as well as incrementing through each comic and saving it to a local Postgres database. This prevents blasting the real XKCD API.

Build with a set of [Dapr components](https://dapr.io).
