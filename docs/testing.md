# Testing

## Scope
This document describes the current test layout and practical test commands.

## Main Command
- Run full suite:
  - `go test ./...`

Current baseline: full suite passes.

## Focused Commands
- HTTP routes and webhook handlers:
  - `go test ./internal/http`
- OpenAPI resolver/build/diff:
  - `go test ./internal/openapi`
- Store + selector behavior:
  - `go test ./internal/store`
- Worker behavior:
  - `go test ./internal/worker`
- End-to-end pipeline integration test package:
  - `go test ./cmd/shiva`

## Coverage Areas
- Config parsing and defaults.
- GitLab API client behavior.
- Inbound webhook validation + ingest persistence behavior.
- Worker retry and permanent-failure handling.
- OpenAPI resolver:
  - incremental path (`compare`, candidate filtering, strict top-level validation, duplicate candidate collapse),
  - bootstrap path (`repository/tree` discovery, `.shivaignore` filtering, per-root dependency closure),
  - `$ref` recursion/cycle/fetch limit guards.
- Canonical spec build and endpoint extraction.
- Semantic diff computation.
- Read route selector resolution and endpoint slice responses.
- Outbound notifier signing, retries, and terminal state behavior.
- End-to-end ingest-to-notify flow in `cmd/shiva/webhook_to_notify_integration_test.go`.

## DB/Query Change Validation
When SQL schema/query files change:
1. Regenerate sqlc code (`sqlc generate`).
2. Run focused store tests (`go test ./internal/store`).
3. Run full suite (`go test ./...`).

## References
- Runtime/setup details: `docs/setup.md`
- Ingest behavior under test: `docs/gitlab.md`
- Endpoint route behavior under test: `docs/endpoints.md`
- Webhook contracts under test: `docs/webhooks.md`
- Schema/query generation workflow: `docs/database.md`
