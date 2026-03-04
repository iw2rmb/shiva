# Operations Runbook

## Scope
- Domain: service operations and release readiness for Shiva runtime.
- Related implementation status: `docs/release-readiness-item-12.md`.

## Required Environment
- Required env vars:
  - `SHIVA_HTTP_ADDR`
  - `SHIVA_DATABASE_URL`
  - `SHIVA_TENANT_KEY`
  - `SHIVA_GITLAB_WEBHOOK_SECRET`
  - `SHIVA_GITLAB_BASE_URL`
- Optional tuning env vars:
  - `SHIVA_WORKER_CONCURRENCY`
  - `SHIVA_OPENAPI_PATH_GLOBS`
  - `SHIVA_OPENAPI_REF_MAX_FETCHES`
  - `SHIVA_OUTBOUND_TIMEOUT_SECONDS`
  - `SHIVA_INGRESS_BODY_LIMIT_BYTES`
  - `SHIVA_INGRESS_RATE_LIMIT_MAX`
  - `SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS`
  - `SHIVA_METRICS_PATH`
  - `SHIVA_TRACING_ENABLED`
  - `SHIVA_TRACING_STDOUT`

## Startup and Health Checks
1. Apply deployment manifest (`deploy/k8s/shiva.yaml`) with environment-specific values.
2. Confirm pod readiness by probing `GET /healthz`.
3. Confirm metrics endpoint (`GET ${SHIVA_METRICS_PATH}`) is reachable from monitoring plane.
4. Send a signed GitLab webhook to `/internal/webhooks/gitlab` and confirm `202` or idempotent `200`.

## Primary Runtime Signals
- Ingest stage:
  - webhook response latency and failure counters,
  - invalid token/signature responses (`401/403`), rate limits (`429`).
- Worker stage:
  - stuck pending ingest events,
  - retries and terminal `failed` status in ingest queue.
- Build stage:
  - OpenAPI resolution failures (`invalid document`, `$ref` cycle/fetch limit, missing files).
- Delivery stage:
  - `delivery_attempts` state distribution: `retry_scheduled`, `failed`, `succeeded`.

## Triage Playbooks
### Webhook ingestion fails
1. Validate GitLab secret alignment (`X-Gitlab-Token` vs `SHIVA_GITLAB_WEBHOOK_SECRET`).
2. Validate payload contains non-zero `after` SHA and `refs/heads/*` branch ref.
3. Check ingress body/rate limits and adjust env vars if valid traffic is throttled.

### Worker backlog grows
1. Check DB connectivity and lock contention.
2. Inspect failed ingest events and error messages for permanent vs retryable faults.
3. Increase `SHIVA_WORKER_CONCURRENCY` only after DB and outbound capacity checks.

### OpenAPI processing fails
1. Re-run failing compare inputs (`repo_id`, `parent_sha`, `sha`) against GitLab API.
2. Confirm changed paths match configured include globs.
3. Validate refs are local and within repo root; check for cycles and excessive ref fanout.

### Outbound notifications fail
1. Verify receiver availability and TLS/network reachability.
2. Validate subscription target URL and secret.
3. Inspect `delivery_attempts` for max attempts reached and dead-letter outcomes.

## Release Checklist
1. `go test ./...` is green.
2. Integration path test passes: `TestIntegrationWebhookToNotifyFlow`.
3. Split-file fixture E2E passes: `TestSplitFileFixtureE2E_ResolveAndBuildCanonicalSpec`.
4. Deployment manifest values are set for target environment.
5. Monitoring and alerting are wired to ingest/build/delivery stages.

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Hardening controls: `docs/hardening-observability-security-controls.md`
- Outbound notifications: `docs/outbound-webhook-notifications.md`
- Release readiness item: `docs/release-readiness-item-12.md`
- Deployment manifest: `deploy/k8s/shiva.yaml`
