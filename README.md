# Shiva

Shiva is a Go service that ingests GitLab push events, detects OpenAPI changes, builds canonical specs, stores endpoint indexes, computes semantic diffs, and sends outbound webhook notifications.

## Current Scope
- Stack: Go + Fiber + PostgreSQL (`pgx` + `sqlc`).
- Processing model: async DB-backed worker with retry/backoff.
- OpenAPI detection: compare-based candidate resolution from GitLab APIs (no full-tree bootstrap discovery yet).
- Read API: selector-based full spec fetch and endpoint operation slices.
- Draft CLI: repo spec fetch and `#operationId` lookup for repos with exactly one active API root.

## HTTP Routes
- `POST /internal/webhooks/gitlab`
- `GET /healthz`
- `GET /internal/metrics` (or configured `SHIVA_METRICS_PATH`)
- `GET /v1/specs/{repo}/{openapi|index}.{json|yaml}`
- `GET /v1/specs/{repo}/{selector}/{openapi|index}.{json|yaml}`
- `GET /v1/specs/{repo}/apis`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{repo}/{path}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{repo}/{selector}/{path}`

Selector semantics:
- `selector` is an 8-character lowercase commit SHA prefix.
- no-selector routes resolve to the latest processed revision on the repo's stored default branch.

Operation-slice semantics:
- request HTTP verb is the endpoint method selector,
- response is JSON by default,
- `{path}.json` and `{path}.yaml` are supported format addons.
- `repo` uses GitLab `path_with_namespace` and must be URL-escaped when it contains `/`.

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
- Draft CLI behavior: [docs/cli.md](docs/cli.md)
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
