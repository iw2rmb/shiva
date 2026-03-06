# Setup

## Scope
This document describes runtime setup, configuration, and startup behavior of the current Shiva codebase.

## Prerequisites
- Go `1.22+`.
- PostgreSQL.
- GitLab access token only if your GitLab APIs require auth.

## Run
- Start service:
  - `go run ./cmd/shiva`
- Run tests:
  - `go test ./...`

## Runtime Behavior
- Shiva expects DB connectivity at startup.
- Missing DB URL or DB connection failure is a startup error.
- Worker pipeline is always enabled when the service starts.

## Environment Variables

### Core
- `SHIVA_HTTP_ADDR` (default `:8080`).
- `SHIVA_DATABASE_URL` (required).
- `SHIVA_TENANT_KEY` (default `default`).
- `SHIVA_LOG_LEVEL` (default `info`).
- `SHIVA_SHUTDOWN_TIMEOUT_SECONDS` (default `15`).

### GitLab + Ingest
- `SHIVA_GITLAB_BASE_URL` (required).
- `SHIVA_GITLAB_TOKEN` (optional).
- `SHIVA_GITLAB_WEBHOOK_SECRET` (required for accepting inbound GitLab webhooks).

### Worker + OpenAPI Resolver
- `SHIVA_WORKER_CONCURRENCY` (default `4`).
- `SHIVA_OPENAPI_PATH_GLOBS` (default: `**/openapi*.{yaml,yml,json},**/swagger*.{yaml,yml,json},**/api/**/*.yaml`).
- `SHIVA_OPENAPI_REF_MAX_FETCHES` (default `128`).
- `SHIVA_OPENAPI_BOOTSTRAP_FETCH_CONCURRENCY` (default `8`, must be `>= 1`).
- `SHIVA_OPENAPI_BOOTSTRAP_SNIFF_BYTES` (default `4096`, must be `>= 1`).

### Outbound Delivery
- `SHIVA_OUTBOUND_TIMEOUT_SECONDS` (default `10`).

### Ingress Controls
- `SHIVA_INGRESS_BODY_LIMIT_BYTES` (default `1048576`).
- `SHIVA_INGRESS_RATE_LIMIT_MAX` (default `120`).
- `SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS` (default `60`).

### Observability
- `SHIVA_METRICS_PATH` (default `/internal/metrics`).
- `SHIVA_TRACING_ENABLED` (default `true`).
- `SHIVA_TRACING_STDOUT` (default `false`).

## Health and Metrics
- `GET /healthz`
  - returns service status and DB health (`ok`, `unreachable`).
- `GET /internal/metrics` (or configured metrics path).

## Deployment Reference
- Kubernetes manifest example: `deploy/k8s/shiva.yaml`.

## References
- GitLab ingest flow: `docs/gitlab.md`
- Endpoint/index and read routes: `docs/endpoints.md`
- Webhook contracts: `docs/webhooks.md`
- Database and sqlc: `docs/database.md`
- Test strategy and commands: `docs/testing.md`
