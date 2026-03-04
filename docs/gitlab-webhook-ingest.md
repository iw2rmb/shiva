# GitLab Webhook Ingest (Item 3)

## Status
- Implemented: `POST /internal/webhooks/gitlab`
- Scope completed:
  - `X-Gitlab-Token` verification
  - delivery id dedupe (`repo_id`, `delivery_id`)
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
- Persistence key for dedupe: unique constraint `(repo_id, delivery_id)` in `ingest_events`.
- First delivery response: `202` with `duplicate=false`.
- Duplicate delivery response: `200` with `duplicate=true` and the existing `event_id`.

## Persistence Flow
1. Resolve tenant by configured `SHIVA_TENANT_KEY` (default `default`), create if absent.
2. Resolve repo by `(tenant_id, gitlab_project_id)`, create if absent.
3. Insert ingest event with `status='pending'`.
4. On unique conflict for `(repo_id, delivery_id)`, fetch and return existing ingest event id.

## Tests
- `TestGitLabWebhookTokenVerification`
- `TestGitLabWebhookDuplicateDeliveryIdempotency`

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Database schema baseline: `docs/database-schema-baseline.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
