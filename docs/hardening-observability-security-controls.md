# Hardening: Observability and Ingress Controls (Item 11)

## Status
- Implemented:
  - structured logs with correlation identifiers across ingest, processing, and notify,
  - stage metrics for ingest/build/delivery latency and failures,
  - tracing spans for ingest/compare/build/diff/notify stages,
  - ingress body-size and rate-limit controls.

## Correlation IDs in Structured Logs
- Correlation fields carried through the webhook-to-notify flow:
  - `delivery_id`
  - `repo_id`
  - `revision_id`
  - `sha`
- Ingest logs:
  - `internal/http/webhook_gitlab.go`
  - successful webhook acceptance and ingest persistence failures include correlation fields.
- Worker/processor logs:
  - `internal/worker/manager.go`
  - queue failure/retry/terminal logs include `delivery_id`, `repo_id`, and `sha`.
- Notify logs:
  - `internal/notify/notifier.go`
  - dispatch success/failure/skip logs include `delivery_id`, `repo_id`, `revision_id`, `sha`, and `event_type`.

## Metrics
- Metric endpoint:
  - `GET /internal/metrics` by default.
  - path is configurable with `SHIVA_METRICS_PATH`.
- Endpoint payload is JSON with fields:
  - `stage_latency_seconds`
  - `stage_failures_total`
- Stage keys:
  - `ingest`
  - `build`
  - `delivery`
- Instrumentation points:
  - `internal/http/webhook_gitlab.go` for ingest.
  - `cmd/shiva/main.go` for build stage.
  - `internal/notify/notifier.go` for delivery stage.

## Tracing
- Added spans:
  - `webhook.ingest`
  - `gitlab.compare`
  - `spec.build`
  - `diff.compute`
  - `notify.dispatch`
- Main instrumentation locations:
  - `internal/http/webhook_gitlab.go`
  - `cmd/shiva/main.go`
  - `internal/notify/notifier.go`
- Tracing runtime controls:
  - `SHIVA_TRACING_ENABLED` (default `true`)
  - `SHIVA_TRACING_STDOUT` (default `false`; enables stdout exporter when `true`)

## Ingress Controls
- Body size limit:
  - config: `SHIVA_INGRESS_BODY_LIMIT_BYTES`
  - default: `1048576` (1 MiB)
  - applied at Fiber app level for request body parsing.
- Rate limit on ingress webhook routes:
  - route group: `/internal/webhooks/*`
  - configs:
    - `SHIVA_INGRESS_RATE_LIMIT_MAX`
    - `SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS`
  - exceeded limit response: `429` with JSON error.

## Config Additions
- `SHIVA_INGRESS_BODY_LIMIT_BYTES`
- `SHIVA_INGRESS_RATE_LIMIT_MAX`
- `SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS`
- `SHIVA_METRICS_PATH`
- `SHIVA_TRACING_ENABLED`
- `SHIVA_TRACING_STDOUT`

## Tests
- `internal/http/webhook_gitlab_test.go`
  - `TestGitLabWebhookIngressBodyLimit`
  - `TestGitLabWebhookIngressRateLimit`
- `internal/observability/metrics_test.go`
  - `TestMetrics_ObserveStagesAndExposeSnapshot`
- `internal/notify/notifier_test.go`
  - `TestDispatchEvent_EmitsNotifyDispatchSpan`

## References
- Runtime baseline: `docs/runtime-baseline.md`
- GitLab webhook ingest: `docs/gitlab-webhook-ingest.md`
- Ingest worker processing: `docs/ingest-worker-processing.md`
- Outbound notifications: `docs/outbound-webhook-notifications.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
