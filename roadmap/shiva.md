# Shiva Implementation Roadmap

## Scope
Implement the Shiva service described in `design/shiva.md` using Go + Fiber + sqlc + pgx, with GitLab Compare API + Repository Files API for OpenAPI detection and retrieval.

## Phase 1: Foundation
1. [x] Bootstrap project structure and runtime wiring.
- Deliverables:
  - `go.mod`
  - `cmd/shiva/main.go`
  - `internal/config`, `internal/http`, `internal/store`, `internal/worker`
  - graceful shutdown, config loading, logger setup
- Exit criteria:
  - service starts and responds on `/healthz`.

2. [x] Create database schema and sqlc generation.
- Deliverables:
  - `sql/schema/*.sql` migrations for tenants/repos/events/revisions/artifacts/index/changes/delivery attempts
  - `sql/query/*.sql`
  - generated sqlc package
- Exit criteria:
  - migrations apply cleanly
  - sqlc code compiles.

## Phase 2: Ingestion and Processing Backbone
3. [x] Implement GitLab webhook ingest endpoint.
- Deliverables:
  - `POST /internal/webhooks/gitlab`
  - secret/token verification
  - delivery id dedupe and event persistence
- Exit criteria:
  - duplicate delivery is idempotent
  - invalid signatures rejected with `401/403`.

4. [x] Implement ordered async processing by repository.
- Deliverables:
  - queue abstraction and worker pool
  - repo-keyed ordering (`repo_id`)
  - idempotency key for `(repo_id, sha)`
- Exit criteria:
  - events for same repo process in commit order
  - retries with backoff work.

## Phase 3: GitLab Retrieval and OpenAPI Build
5. [x] Implement GitLab client integrations.
- Deliverables:
  - compare endpoint client for changed paths
  - repository files endpoint client for content by `ref=sha`
- Exit criteria:
  - no archive/clone usage
  - changed file list and file fetch are both covered by tests.

6. [x] Implement OpenAPI candidate detection and `$ref` resolution.
- Deliverables:
  - configurable include globs
  - top-level `openapi`/`swagger` validation
  - recursive local `$ref` fetch via Repository Files API
  - cycle detection and fetch limits
- Exit criteria:
  - multi-file spec resolves correctly
  - invalid/cyclic refs fail with explicit error.

7. [x] Implement canonical spec build and persistence.
- Deliverables:
  - canonical JSON + YAML outputs
  - `spec_artifacts` storage
  - `endpoint_index` extraction
- Exit criteria:
  - identical input produces stable canonical output
  - endpoint routes can read indexed data.

## Phase 4: Diff and Notifications
8. [x] Implement semantic diff engine.
- Deliverables:
  - compare previous processed revision vs current
  - structured `spec_changes` JSON
- Exit criteria:
  - detects added/removed/changed endpoints and parameter/schema changes.

9. [x] Implement outbound webhook notifications.
- Deliverables:
  - events `spec.updated.full`, `spec.updated.diff`
  - HMAC signing + timestamp headers
  - retry/backoff + dead-letter status in `delivery_attempts`
- Exit criteria:
  - delivery retries and terminal failure states observable in DB.

## Phase 5: Read API
10. Implement selector resolution and read routes.
- Deliverables:
  - `GET /{tenant}/{repo}/{selector}/spec.json`
  - `GET /{tenant}/{repo}/{selector}/spec.yaml`
  - `GET /{tenant}/{repo}/{selector}/endpoints`
  - `GET /{tenant}/{repo}/{selector}/endpoints/{method}/{path}`
  - `GET /{tenant}/{repo}/endpoints` => latest processed revision on `main`
- Exit criteria:
  - `sha`, `branch`, `latest`, and no-selector behavior pass integration tests
  - proper `404`/`409` handling and `ETag` support.

## Phase 6: Hardening
11. Add observability and security controls.
- Deliverables:
  - structured logs with correlation ids
  - metrics for ingest/build/delivery latency and failures
  - tracing spans across ingest/process/build/notify
  - ingress rate limit + body size limits
- Exit criteria:
  - dashboards/queries can identify failing stage within one request path.

12. Complete test suite and release readiness.
- Deliverables:
  - unit tests for selector detection/diff/openapi resolution
  - integration tests for end-to-end webhook-to-notify flow
  - fixture-based E2E for split-file OpenAPI
  - runbook and minimal deployment manifest
- Exit criteria:
  - CI is green
  - critical path covered by automated tests.

## Suggested Execution Order
1. Phases 1-2 (platform + ingest + queue)
2. Phase 3 (GitLab retrieval + build)
3. Phase 4 (diff + outbound)
4. Phase 5 (read API)
5. Phase 6 (hardening + release)
