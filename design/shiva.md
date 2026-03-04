# Shiva Design Doc

## Executive Summary
Shiva is a Go service that tracks OpenAPI changes in GitLab repositories and serves versioned API specs to downstream systems.
It receives GitLab commit webhooks, detects OpenAPI file changes, rebuilds full specs for a revision, computes semantic changes, and notifies subscribed webhooks.
It also exposes tenant-scoped routes for fetching full specs and endpoint-level views by `sha`, `branch`, or `latest`.

## Goals
- Ingest GitLab push events and process them asynchronously.
- Detect whether OpenAPI source files changed between revisions.
- Build a canonical full spec (`json` + `yaml`) for each tracked revision.
- Emit update events (full spec and/or change set) to registered outbound webhooks.
- Serve read APIs:
  - `/{tenant}/{repo}/{sha|branch|latest}/spec.{json|yaml}`
  - `/{tenant}/{repo}/{sha|branch|latest}/endpoints`
  - `/{tenant}/{repo}/{sha|branch|latest}/endpoints/{method}/{path}`

## Non-Goals
- Backward compatibility for undocumented legacy payload formats.
- Full Git hosting abstraction beyond GitLab in v1.
- Full OpenAPI linting/style enforcement (validation only for parse/build correctness).

## High-Level Architecture
1. **Ingress API (Fiber)**
   - Accepts GitLab webhooks.
   - Verifies signature/token.
   - Persists event and enqueues processing.

2. **Change Processor**
   - Resolves commit range.
   - Fetches changed files from GitLab compare API.
   - Filters OpenAPI candidates by path pattern and content.
   - If changed: fetches only required OpenAPI files at target revision via GitLab Repository Files API and rebuilds canonical spec.

3. **Spec Builder**
   - Supports single-file and split-file OpenAPI (`$ref`) layouts.
   - Produces canonical JSON and YAML artifacts.
   - Stores normalized endpoint index for fast route serving.

4. **Diff Engine**
   - Compares current canonical spec with previous tracked revision.
   - Emits structured change model (added/removed/changed endpoints, params, schemas).

5. **Outbound Notifier**
   - Looks up subscriber webhooks per tenant/repo.
   - Dispatches signed event payloads with retry/backoff and dead-letter state.

6. **Read API (Fiber)**
   - Resolves version selector (`sha|branch|latest`) to stored artifact.
   - Returns spec files and endpoint-derived responses.

## Data Model (PostgreSQL via sqlc + pgx)
- `tenants`
  - `id`, `key`, timestamps.
- `repos`
  - `id`, `tenant_id`, `gitlab_project_id`, `path_with_namespace`, `default_branch`, timestamps.
- `subscriptions`
  - `id`, `tenant_id`, `repo_id`, `target_url`, `secret`, `enabled`, retry policy fields.
- `ingest_events`
  - `id`, `tenant_id`, `repo_id`, `event_type`, `delivery_id`, `payload_json`, `received_at`, `status`.
- `revisions`
  - `id`, `repo_id`, `sha`, `branch`, `parent_sha`, `processed_at`, `openapi_changed`.
- `spec_artifacts`
  - `id`, `revision_id`, `spec_json`, `spec_yaml`, `etag`, `size_bytes`.
- `endpoint_index`
  - `id`, `revision_id`, `method`, `path`, `operation_id`, `summary`, `deprecated`, `raw_json`.
- `spec_changes`
  - `id`, `repo_id`, `from_revision_id`, `to_revision_id`, `change_json`, `created_at`.
- `delivery_attempts`
  - `id`, `subscription_id`, `revision_id`, `attempt_no`, `status`, `response_code`, `error`, `next_retry_at`.

## Inbound Flow
1. GitLab sends push webhook to `POST /internal/webhooks/gitlab`.
2. Service validates secret/token and deduplicates by delivery id.
3. Event persisted, then processor job enqueued.
4. Processor computes changed files for commit range.
5. If no OpenAPI impact: mark revision as processed and stop.
6. If OpenAPI changed:
   - Fetch changed candidates and `$ref`-required files via Repository Files API at head SHA.
   - Build canonical full spec for head SHA.
   - Store artifacts + endpoint index.
   - Compute diff vs previous known revision.
   - Queue outbound deliveries.

## Outbound Event Contract
- Event types:
  - `spec.updated.full`
  - `spec.updated.diff`
- Common fields:
  - `tenant`, `repo`, `sha`, `branch`, `processed_at`, `event_id`.
- Full payload:
  - `spec_url_json`, `spec_url_yaml`, `etag`.
- Diff payload:
  - `from_sha`, `to_sha`, `changes`.
- Delivery security:
  - HMAC signature header over body using subscription secret.
  - Timestamp header for replay protection.

## Public Read API
Tenant-scoped and repo-scoped:
- `GET /{tenant}/{repo}/{selector}/spec.json`
- `GET /{tenant}/{repo}/{selector}/spec.yaml`
- `GET /{tenant}/{repo}/{selector}/endpoints`
- `GET /{tenant}/{repo}/{selector}/endpoints/{method}/{path}`
- `GET /{tenant}/{repo}/endpoints` (latest processed revision on `main`)

Selector resolution:
- `sha`: exact immutable revision.
- `branch`: latest processed revision for branch.
- `latest`: latest processed revision for repo default branch.
- no selector (`/{tenant}/{repo}/endpoints`): latest processed revision on `main`.

Response behavior:
- Strong `ETag` on spec responses.
- `404` if selector has no processed artifact.
- `409` if selector points to unprocessed revision.

## OpenAPI Change Detection Strategy
Primary signals:
- Changed path matches configurable include globs:
  - `**/openapi*.{yaml,yml,json}`
  - `**/swagger*.{yaml,yml,json}`
  - `**/api/**/*.yaml`
- File content parse check for `openapi` or `swagger` top-level keys.

If any candidate changed:
- Perform full rebuild at target SHA instead of patching partial artifacts.
- This avoids inconsistent `$ref` graph state.

## GitLab Retrieval Strategy
- Do not download repository archives or clone full repos.
- Use GitLab Compare API to identify changed paths in the commit range.
- For each candidate OpenAPI document, fetch file content with GitLab Repository Files API at `ref=<head_sha>`.
- Parse document and recursively fetch local `$ref` targets with Repository Files API until dependency closure is complete.
- Enforce limits for safety:
  - max referenced file count per build,
  - max aggregate bytes fetched,
  - cycle detection for `$ref` graph.

## Processing Model
- Asynchronous worker pool, keyed by `repo_id` to preserve commit order per repo.
- Idempotency keys:
  - ingress: `(repo_id, delivery_id)`
  - build: `(repo_id, sha)`
  - outbound: `(subscription_id, revision_id, event_type)`
- Retries:
  - Exponential backoff with jitter.
  - Max attempts configurable per subscription.
  - Dead-letter state retained for operator replay.

## Security
- Inbound GitLab webhook authentication (token/signature).
- Outbound webhook signing (HMAC-SHA256).
- Tenant isolation in all queries (`tenant_id` guard).
- Rate limiting and body size limits on ingress.
- Optional allowlist for outbound webhook hosts.

## Observability
- Structured logs with correlation ids (`delivery_id`, `revision_id`).
- Metrics:
  - ingest rate, processing latency, build failures, notification failures.
- Tracing spans:
  - `webhook.ingest`, `gitlab.compare`, `spec.build`, `diff.compute`, `notify.dispatch`.

## Failure Modes and Handling
- GitLab API transient failure: retry job with backoff.
- Invalid OpenAPI after change: mark revision failed, emit operator event, do not publish new artifact.
- Outbound endpoint failure: retry until max attempts, then dead-letter.
- Database write failure: transaction rollback and retry job.

## Project Layout (Proposed)
- `cmd/shiva/main.go`
- `internal/http` (Fiber handlers, middleware)
- `internal/gitlab` (API client)
- `internal/openapi` (loader, canonicalizer, endpoint extractor, diff)
- `internal/worker` (queue consumers)
- `internal/notify` (webhook delivery)
- `internal/store` (sqlc-generated queries + repository wrappers)
- `sql/schema` (migrations)
- `sql/query` (sqlc query files)
- `design/shiva.md`

## Configuration
- `SHIVA_HTTP_ADDR`
- `SHIVA_DATABASE_URL`
- `SHIVA_GITLAB_BASE_URL`
- `SHIVA_GITLAB_TOKEN`
- `SHIVA_GITLAB_WEBHOOK_SECRET`
- `SHIVA_WORKER_CONCURRENCY`
- `SHIVA_OPENAPI_PATH_GLOBS`
- `SHIVA_OUTBOUND_TIMEOUT`
- `SHIVA_OUTBOUND_MAX_RETRIES`

## Testing Strategy
- Unit:
  - selector resolution, OpenAPI detection, endpoint extraction, diff logic.
- Integration:
  - webhook ingest -> build -> artifact store -> notify flow using test DB + mocked GitLab.
- Contract:
  - outbound payload schema tests for subscribers.
- E2E:
  - push event fixture with multi-file OpenAPI refs.

## Milestones
1. Core ingest + persistence + processing pipeline.
2. Canonical spec build + storage + read routes.
3. Diff computation + outbound notifications.
4. Hardening: retries, observability, security controls.
