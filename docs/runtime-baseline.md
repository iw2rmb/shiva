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
- `SHIVA_OUTBOUND_TIMEOUT_SECONDS` (default `10`)
- `SHIVA_LOG_LEVEL` (default `info`, values: `debug`, `info`, `warn`, `error`)
- `SHIVA_OPENAPI_PATH_GLOBS` (optional comma-separated include globs)
- `SHIVA_OPENAPI_REF_MAX_FETCHES` (default `128`)
- `SHIVA_INGRESS_BODY_LIMIT_BYTES` (default `1048576`)
- `SHIVA_INGRESS_RATE_LIMIT_MAX` (default `120`)
- `SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS` (default `60`)
- `SHIVA_METRICS_PATH` (default `/internal/metrics`)
- `SHIVA_TRACING_ENABLED` (default `true`)
- `SHIVA_TRACING_STDOUT` (default `false`)

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
- Outbound notifications for item 9 are documented in `docs/outbound-webhook-notifications.md`.
- Read API selector routes (item 10) are implemented; see `docs/read-api-selector-resolution.md`.
- Hardening controls from item 11 are implemented; see `docs/hardening-observability-security-controls.md`.
- Release-readiness test expansion (item 12) is not implemented yet.
- Database baseline artifacts are documented in `docs/database-schema-baseline.md`.
- See `design/shiva.md` for the broader architecture and `roadmap/shiva.md` for remaining items.
