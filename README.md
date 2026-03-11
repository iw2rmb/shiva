# Shiva

Shiva is a Go service that ingests GitLab push events, detects OpenAPI changes, builds canonical specs, stores endpoint indexes, computes semantic diffs, and sends outbound webhook notifications.

## Current Scope
- Stack: Go + Fiber + PostgreSQL (`pgx` + `sqlc`).
- Processing model: async DB-backed worker with retry/backoff.
- OpenAPI detection: compare-based candidate resolution from GitLab APIs (no full-tree bootstrap discovery yet).
- Read API: query-driven spec, operation, API inventory, operation inventory, repo inventory, and catalog freshness endpoints.
- CLI: shipped shorthand inspect/call grammar, XDG-backed profiles and targets, catalog/cache refresh, list/sync/batch flows, and dynamic completion.

## HTTP Routes
- `POST /internal/webhooks/gitlab`
- `GET /healthz`
- `GET /internal/metrics` (or configured `SHIVA_METRICS_PATH`)
- `GET /v1/spec`
- `GET /v1/operation`
- `POST /v1/call`
- `GET /v1/apis`
- `GET /v1/operations`
- `GET /v1/repos`
- `GET /v1/catalog/status`

Query semantics:
- `repo` uses raw GitLab `path_with_namespace`.
- optional snapshot selection uses either `revision_id`, `sha`, or neither.
- `sha` is an 8-character lowercase commit SHA prefix.
- omitting snapshot selectors resolves the latest processed OpenAPI snapshot on the repo's stored default branch.
- `/v1/operation` accepts either `operation_id` or `method` plus `path`.
- `/v1/spec` supports `format=json|yaml` and `ETag`/`If-None-Match`.

## Quick Start
1. Set required envs for full mode:
   - `SHIVA_DATABASE_URL`
   - `SHIVA_GITLAB_BASE_URL`
   - `SHIVA_GITLAB_WEBHOOK_SECRET`
2. Start:
   - `go run ./cmd/shivad`
3. Validate:
   - `go test ./...`

## Documentation
- Setup and configuration: [docs/setup.md](docs/setup.md)
- CLI behavior: [docs/cli.md](docs/cli.md)
- GitLab spec ingestion flow: [docs/gitlab.md](docs/gitlab.md)
- Endpoint extraction and query transport: [docs/endpoints.md](docs/endpoints.md)
- Inbound/outbound webhook contracts: [docs/webhooks.md](docs/webhooks.md)
- Test layout and commands: [docs/testing.md](docs/testing.md)
- Database schema and sqlc generation: [docs/database.md](docs/database.md)

## Design and Roadmap
- Architecture/design: [design/shiva.md](design/shiva.md)
- Monorepo/bootstrap design draft: [design/mono.md](design/mono.md)
- Implementation roadmap: [roadmap/shiva.md](roadmap/shiva.md)

## Deployment
- Kubernetes example manifest: [deploy/k8s/shiva.yaml](deploy/k8s/shiva.yaml)
