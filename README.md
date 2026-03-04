# Shiva

Shiva is a Go service that tracks OpenAPI changes in GitLab repositories, rebuilds canonical specs, and distributes updates to subscribers. It also serves tenant-scoped, versioned routes for full specs and endpoint-level views.

## Current State
- Roadmap status: fully implemented (`roadmap/shiva.md` items 1-12 are complete).
- Stack: Go, Fiber, PostgreSQL (sqlc + pgx), async worker pipeline.
- CI/test baseline: full test suite runs with `go test ./...`.

## Implemented Capabilities
- GitLab webhook ingest with token verification and idempotent delivery handling.
- Repo-ordered async processing with retries/backoff.
- GitLab Compare + Repository Files API retrieval (no clone/archive flow).
- OpenAPI candidate detection and recursive local `$ref` resolution with cycle/fetch limits.
- Canonical spec build (`json` + `yaml`) and endpoint index persistence.
- Semantic diff persistence (`spec_changes`) with structured endpoint/parameter/schema changes.
- Outbound notifications:
  - event types: `spec.updated.full`, `spec.updated.diff`
  - HMAC signing (`X-Shiva-Signature`) + timestamp header (`X-Shiva-Timestamp`)
  - retry/backoff and terminal failure state in `delivery_attempts`
- Selector-based read API with `404/409` semantics and `ETag`/`If-None-Match` handling.
- Hardening controls:
  - correlation IDs in structured logs
  - ingest/build/delivery metrics at configurable metrics path
  - tracing spans across ingest/process/build/diff/notify
  - ingress body-size and rate limiting

## HTTP Routes
- `POST /internal/webhooks/gitlab`
- `GET /healthz`
- `GET /internal/metrics` (path configurable via `SHIVA_METRICS_PATH`)
- `GET /{tenant}/{repo}.{json|yaml}`
- `GET /{tenant}/{repo}/{selector}.{json|yaml}`
- `GET /{tenant}/{repo}/{method}/{path}`
- `GET /{tenant}/{repo}/{selector}/{method}/{path}`
- `GET /{tenant}/{repo}/{method}.{json|yaml}`
- `GET /{tenant}/{repo}/{selector}/{method}.{json|yaml}`

`selector` is one of:
- commit SHA
- branch name
- `latest` (default branch latest processed revision)

No-selector routes default to latest processed revision on `main`.

## Configuration
Environment variables are documented in [docs/runtime-baseline.md](docs/runtime-baseline.md), including:
- runtime and worker settings
- GitLab integration settings
- outbound timeout
- ingress rate/body limits
- metrics/tracing controls

## Documentation Map
- Architecture/design: [design/shiva.md](design/shiva.md)
- Implementation roadmap: [roadmap/shiva.md](roadmap/shiva.md)
- Runtime/config baseline: [docs/runtime-baseline.md](docs/runtime-baseline.md)
- Webhook ingest: [docs/gitlab-webhook-ingest.md](docs/gitlab-webhook-ingest.md)
- Worker processing: [docs/ingest-worker-processing.md](docs/ingest-worker-processing.md)
- OpenAPI resolution: [docs/openapi-candidate-resolution.md](docs/openapi-candidate-resolution.md)
- Canonical artifacts/index: [docs/canonical-spec-build-persistence.md](docs/canonical-spec-build-persistence.md)
- Semantic diff: [docs/semantic-diff-engine.md](docs/semantic-diff-engine.md)
- Outbound notifications: [docs/outbound-webhook-notifications.md](docs/outbound-webhook-notifications.md)
- Read API selectors/routes: [docs/read-api-selector-resolution.md](docs/read-api-selector-resolution.md)
- Hardening/observability/security: [docs/hardening-observability-security-controls.md](docs/hardening-observability-security-controls.md)
- Release readiness: [docs/release-readiness-item-12.md](docs/release-readiness-item-12.md)
- Operations runbook: [docs/operations-runbook.md](docs/operations-runbook.md)

## Deployment
- Minimal Kubernetes manifest: [deploy/k8s/shiva.yaml](deploy/k8s/shiva.yaml)
