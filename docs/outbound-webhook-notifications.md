# Outbound Webhook Notifications (Item 9)

## Status
- Implemented: outbound webhook delivery for OpenAPI-changed revisions.
- Scope completed:
  - emit both events `spec.updated.full` and `spec.updated.diff`,
  - sign outbound requests with HMAC-SHA256 over request body,
  - attach timestamp header on outbound requests,
  - persist retry/backoff and terminal failure state in `delivery_attempts`.
- Explicitly out of scope for this item:
  - selector/read API routes (item 10),
  - release-readiness test expansion from item 12.

## Processor Integration
- File: `cmd/shiva/main.go`
- Integration point:
  - only when `OpenAPIChanged=true`,
  - after canonical spec and semantic diff persistence,
  - after revision is marked `processed`.
- Dispatch source of truth:
  - loads persisted `spec_artifacts` and `spec_changes` for `revision_id`,
  - resolves `from_sha` via `from_revision_id` when available.

## Outbound Contract
- Event types:
  - `spec.updated.full`
  - `spec.updated.diff`
- Envelope fields:
  - `type`
  - `event_id`
  - `tenant`
  - `repo`
  - `revision_id`
  - `sha`
  - `branch`
  - `processed_at`
  - `payload`
- `spec.updated.full` payload:
  - `etag`
  - `size_bytes`
  - `spec_json`
  - `spec_yaml`
- `spec.updated.diff` payload:
  - `from_revision_id` (nullable),
  - `from_sha` (empty when no baseline),
  - `to_revision_id`,
  - `to_sha`,
  - `changes` (from `spec_changes.change_json`).

## Security Headers
- Timestamp header: `X-Shiva-Timestamp` (`RFC3339Nano`, UTC).
- Signature header: `X-Shiva-Signature`.
- Signature format: `sha256=<hex>`.
- Signature input: exact outbound request body bytes.

## Delivery Attempt State Machine
- Delivery rows are tracked per `(subscription_id, revision_id, event_type)` as attempts with incrementing `attempt_no`.
- State transitions:
  - `pending` (attempt row created),
  - `succeeded` on `2xx` response,
  - `retry_scheduled` on retryable failure with `next_retry_at`,
  - `failed` on terminal failure (dead-letter state).
- Retryable failures:
  - network/dispatch error,
  - non-`2xx` HTTP response.
- Non-retryable failures:
  - invalid `target_url`,
  - empty subscription secret.
- Backoff:
  - exponential doubling from `subscriptions.backoff_initial_seconds`,
  - capped by `subscriptions.backoff_max_seconds`.
- Attempt cap:
  - `subscriptions.max_attempts`.

## Idempotency and Retry Safety
- Before dispatch, latest attempt is loaded for `(subscription_id, revision_id, event_type)`.
- If latest status is terminal (`succeeded` or `failed`), event dispatch is skipped.
- If latest status is non-terminal, next attempt continues from `latest.attempt_no + 1`.
- Re-running processor for the same revision does not re-send terminally completed events.

## Components
- Notifier implementation:
  - `internal/notify/notifier.go`
  - with item-11 hardening integration:
    - `notify.dispatch` tracing span per dispatch path,
    - delivery stage latency/failure metrics,
    - structured dispatch logs with correlation fields (`delivery_id`, `repo_id`, `revision_id`, `sha`).
- Store wrappers:
  - `internal/store/subscriptions.go`
  - `internal/store/delivery_attempts.go`
  - `internal/store/spec_artifacts.go`
  - `internal/store/spec_changes.go`
  - `internal/store/revisions.go`
  - `internal/store/tenants.go`
- SQL/query support:
  - `sql/query/delivery_attempts.sql`
  - `sql/query/revisions.sql`
  - `sql/query/tenants.sql`

## Tests
- `internal/notify/notifier_test.go`
  - `TestNotifierNotifyRevision_EmitsFullAndDiffWithSigning`
  - `TestDispatchEvent_RetryAndTerminalStates` (table-driven)
  - `TestDispatchEvent_SkipsTerminalAttempt`
  - `TestDispatchEvent_EmitsNotifyDispatchSpan`
  - `TestCalculateBackoff` (table-driven)

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Worker processing: `docs/ingest-worker-processing.md`
- Hardening controls: `docs/hardening-observability-security-controls.md`
- Canonical build + persistence: `docs/canonical-spec-build-persistence.md`
- Semantic diff engine: `docs/semantic-diff-engine.md`
- Database schema baseline: `docs/database-schema-baseline.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
