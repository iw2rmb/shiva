# Webhooks

## Scope
This document defines webhook contracts used by Shiva:
- inbound GitLab webhook,
- outbound subscriber webhooks.

## Inbound GitLab Webhook
Route:
- `POST /internal/webhooks/gitlab`

Required headers:
- `X-Gitlab-Token` (must equal `SHIVA_GITLAB_WEBHOOK_SECRET`),
- delivery id from first non-empty header:
  - `X-Gitlab-Delivery`,
  - `X-Gitlab-Event-UUID`,
  - `X-Gitlab-Webhook-UUID`.

Payload requirements:
- valid JSON,
- `after` must be non-zero commit SHA,
- `ref` must be `refs/heads/<branch>`.

Response behavior:
- `202` accepted for new event,
- `200` accepted for duplicate event,
- `401` missing token header,
- `403` invalid token,
- `400` invalid payload/headers,
- `503` when webhook secret or store is not configured.

Deduplication is DB-backed by unique `(repo_id, delivery_id)` and `(repo_id, sha)`.

## Outbound Subscriber Webhooks
Triggered only after `openapi_changed=true` revision processing.

Event types:
- `spec.updated.full`
- `spec.updated.diff`

Per event attempt headers:
- `Content-Type: application/json`
- `X-Shiva-Timestamp: <RFC3339Nano UTC>`
- `X-Shiva-Signature: sha256=<hex(hmac_sha256(secret, raw_body))>`
- `X-Shiva-Event-ID: sub:<subscription_id>:api:<api_spec_id>:rev:<revision_id>:event:<event_type>`

Outgoing payload fields:
- `namespace`: repository namespace path.
- `repo`: repository slug.
- `revision_id`: canonical repository revision id that triggered the webhook (`ingest_events.id`).
- `api`: API root path that changed.
- `api_revision_id`: API spec revision id that changed.
- `sha`: revision SHA at delivery time.
- `branch`: source branch for the revision.
- `processed_at`: RFC3339Nano UTC timestamp.
- `event_id`: envelope id in format `api:<api_spec_id>:rev:<revision_id>:event:<event_type>`.
- `payload`: event-specific payload.

Delivery model:
- subscription list is loaded from enabled repo subscriptions,
- attempts are persisted in `delivery_attempts`,
- statuses: `pending`, `retry_scheduled`, `succeeded`, `failed`,
- retries use exponential backoff bounded by subscription settings,
- terminal `succeeded/failed` blocks duplicate redispatch for the same `(subscription_id, api_spec_id, revision_id, event_type)` key.
- API-scoped identity/payload checks:
  - dedupe and retry identity is `(subscription_id, api_spec_id, revision_id, event_type)`,
  - header `X-Shiva-Event-ID` is `sub:<subscription_id>:api:<api_spec_id>:rev:<revision_id>:event:<event_type>`,
  - payload is tied to one API via `api` and `api_revision_id`.

Incremental edge behavior:
- root-local permanent errors are isolated to `api_spec_revisions` and do not block outbound delivery if another root in the same revision succeeds (`openapi_changed=true`).
- deleted-root-only revisions emit `spec.updated.diff` even when no canonical artifact exists for the revision.
- `openapi_changed=false` (unchanged/no-impact changes, or fallback miss) results in no outbound webhook events.

## References
- Ingestion and resolver flow: `docs/gitlab.md`
- Runtime setup and secrets: `docs/setup.md`
- Endpoint/read route behavior after processing: `docs/endpoints.md`
- Delivery tables and state: `docs/database.md`
- Webhook and notifier tests: `docs/testing.md`
