# Runtime Baseline (Item 1)

## Status
- Implemented: `go.mod`, `cmd/shiva/main.go`, `internal/config`, `internal/http`, `internal/store`, `internal/worker`.
- Scope completed: bootstrap project startup, graceful shutdown, config loading, logger setup, `/healthz` endpoint.

## Runtime wiring
- `cmd/shiva/main.go` wires configuration, logger, store, worker manager, and HTTP server.
- Lifecycle uses `context.WithCancel` and OS signal handling (`SIGINT`, `SIGTERM`).
- Graceful shutdown applies `SHIVA_SHUTDOWN_TIMEOUT_SECONDS` to stop worker and HTTP server.

## Configuration
- `SHIVA_HTTP_ADDR` (default `:8080`)
- `SHIVA_DATABASE_URL` (optional for this baseline; empty means unconfigured store)
- `SHIVA_WORKER_CONCURRENCY` (default `4`)
- `SHIVA_SHUTDOWN_TIMEOUT_SECONDS` (default `15`)
- `SHIVA_LOG_LEVEL` (default `info`, values: `debug`, `info`, `warn`, `error`)

## Health endpoint
- `GET /healthz` returns `200` and JSON payload.
- Current payload includes:
  - `status`
  - `service`
  - `store.status` (`not_configured`, `ok`, `unreachable`)

## Notes
- No webhook ingest, GitLab client, SQL queries, or read routes are implemented yet.
- See `design/shiva.md` for the broader architecture and `roadmap/shiva.md` for remaining items.
