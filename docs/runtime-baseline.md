# Runtime Baseline (Item 1)

## Status
- Implemented: `go.mod`, `cmd/shiva/main.go`, `internal/config`, `internal/http`, `internal/store`, `internal/worker`.
- Scope completed: bootstrap project startup, graceful shutdown, config loading, logger setup, `/healthz` endpoint.

## Runtime wiring
- `cmd/shiva/main.go` wires configuration, logger, store, worker manager, and HTTP server.
- Lifecycle uses `context.WithCancel` and OS signal handling (`SIGINT`, `SIGTERM`).
- Graceful shutdown applies `SHIVA_SHUTDOWN_TIMEOUT_SECONDS` to stop worker and HTTP server.
- Worker startup is conditional on configured database connectivity:
  - configured DB: starts async ingest worker pool
  - no DB URL: worker processing is disabled and HTTP-only mode still starts.

## Configuration
- `SHIVA_HTTP_ADDR` (default `:8080`)
- `SHIVA_DATABASE_URL` (optional for this baseline; empty means unconfigured store)
- `SHIVA_GITLAB_BASE_URL` (required when DB-backed worker is enabled)
- `SHIVA_GITLAB_TOKEN` (optional GitLab API token)
- `SHIVA_WORKER_CONCURRENCY` (default `4`)
- `SHIVA_SHUTDOWN_TIMEOUT_SECONDS` (default `15`)
- `SHIVA_LOG_LEVEL` (default `info`, values: `debug`, `info`, `warn`, `error`)
- `SHIVA_OPENAPI_PATH_GLOBS` (optional comma-separated include globs)
- `SHIVA_OPENAPI_REF_MAX_FETCHES` (default `128`)

## Health endpoint
- `GET /healthz` returns `200` and JSON payload.
- Current payload includes:
  - `status`
  - `service`
  - `store.status` (`not_configured`, `ok`, `unreachable`)

## Notes
- GitLab webhook ingest for item 3 is documented in `docs/gitlab-webhook-ingest.md`.
- Ordered async ingest processing for item 4 is documented in `docs/ingest-worker-processing.md`.
- GitLab API retrieval client for item 5 is documented in `docs/gitlab-client-integrations.md`.
- OpenAPI candidate detection and `$ref` resolution for item 6 are documented in `docs/openapi-candidate-resolution.md`.
- Canonical artifact build/persistence for item 7 is documented in `docs/canonical-spec-build-persistence.md`.
- Semantic diff engine for item 8 is documented in `docs/semantic-diff-engine.md`.
- Outbound notifications and read API work (items 9-10) are not implemented yet.
- Database baseline artifacts are documented in `docs/database-schema-baseline.md`.
- See `design/shiva.md` for the broader architecture and `roadmap/shiva.md` for remaining items.
