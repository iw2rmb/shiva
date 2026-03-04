# GitLab Webhook Ingest (Item 3)

## Status
- Implemented: `POST /internal/webhooks/gitlab`
- Scope completed:
  - `X-Gitlab-Token` verification
  - delivery id dedupe (`repo_id`, `delivery_id`)
  - commit id dedupe (`repo_id`, `sha`)
  - ingest event persistence via sqlc store queries

## Route
- `POST /internal/webhooks/gitlab`

## Authentication
- Config source: `SHIVA_GITLAB_WEBHOOK_SECRET`
- Header: `X-Gitlab-Token`
- Behavior:
  - missing token: `401`
  - invalid token: `403`
  - secret not configured: `503`

## Delivery Idempotency
- Delivery id is read from first non-empty header in:
  - `X-Gitlab-Delivery`
  - `X-Gitlab-Event-UUID`
  - `X-Gitlab-Webhook-UUID`
- Persistence keys for dedupe in `ingest_events`:
  - `(repo_id, delivery_id)`
  - `(repo_id, sha)`
- First delivery response: `202` with `duplicate=false`.
- Duplicate delivery response: `200` with `duplicate=true` and the existing `event_id`.

## Commit Identity Extraction
- Required payload fields:
  - `after`: target commit sha
  - `ref`: branch ref in `refs/heads/*` format
- Optional payload field:
  - `before`: parent sha (all-zero value is normalized to empty).
- If `after` is missing/all-zero or `ref` is invalid, request is rejected with `400`.

## Persistence Flow
1. Resolve tenant by configured `SHIVA_TENANT_KEY` (default `default`), create if absent.
2. Resolve repo by `(tenant_id, gitlab_project_id)`, create if absent.
3. Insert ingest event with `status='pending'`, `attempt_count=0`, and `next_retry_at=NOW()`.
4. On unique conflict for `(repo_id, delivery_id)` or `(repo_id, sha)`, fetch and return existing ingest event id.

## Tests
- `TestGitLabWebhookTokenVerification`
- `TestGitLabWebhookDuplicateDeliveryIdempotency`

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Database schema baseline: `docs/database-schema-baseline.md`
- Worker processing queue: `docs/ingest-worker-processing.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
