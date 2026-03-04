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
- GitLab webhook ingest for item 3 is documented in `docs/gitlab-webhook-ingest.md`.
- GitLab client, async processing queue/order/retries, and read routes are still not implemented.
- Database baseline artifacts are documented in `docs/database-schema-baseline.md`.
- See `design/shiva.md` for the broader architecture and `roadmap/shiva.md` for remaining items.
