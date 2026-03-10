# Shiva

Shiva is a Go service that ingests GitLab push events, detects OpenAPI changes, builds canonical specs, stores endpoint indexes, computes semantic diffs, and sends outbound webhook notifications.

## Current Scope
- Stack: Go + Fiber + PostgreSQL (`pgx` + `sqlc`).
- Processing model: async DB-backed worker with retry/backoff.
- OpenAPI detection: compare-based candidate resolution from GitLab APIs (no full-tree bootstrap discovery yet).
- Read API: selector-based full spec fetch and endpoint operation slices.

## HTTP Routes
- `POST /internal/webhooks/gitlab`
- `GET /healthz`
- `GET /internal/metrics` (or configured `SHIVA_METRICS_PATH`)
- `GET /{tenant}/{repo}.{json|yaml}`
- `GET /{tenant}/{repo}/{selector}.{json|yaml}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /{tenant}/{repo}/{path}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /{tenant}/{repo}/{selector}/{path}`

Selector semantics:
- `selector` is commit SHA, branch, or `latest`.
- no-selector routes resolve to latest processed revision on `main`.

Operation-slice semantics:
- request HTTP verb is the endpoint method selector,
- response is JSON by default,
- `{path}.json` and `{path}.yaml` are supported format addons.

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
- GitLab spec ingestion flow: [docs/gitlab.md](docs/gitlab.md)
- Endpoint extraction and read routes: [docs/endpoints.md](docs/endpoints.md)
- Inbound/outbound webhook contracts: [docs/webhooks.md](docs/webhooks.md)
- Test layout and commands: [docs/testing.md](docs/testing.md)
- Database schema and sqlc generation: [docs/database.md](docs/database.md)

## Design and Roadmap
- Architecture/design: [design/shiva.md](design/shiva.md)
- Monorepo/bootstrap design draft: [design/mono.md](design/mono.md)
- Implementation roadmap: [roadmap/shiva.md](roadmap/shiva.md)

## Deployment
- Kubernetes example manifest: [deploy/k8s/shiva.yaml](deploy/k8s/shiva.yaml)
