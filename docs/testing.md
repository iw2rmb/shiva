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
  - incremental entrypoints for known-root rebuild (`ResolveRootOpenAPIAtSHA`) and explicit-path targeted discovery (`ResolveDiscoveredRootsAtPaths`),
  - bootstrap path (`repository/tree` discovery, `.shivaignore` filtering, bounded candidate fetch concurrency, deterministic root ordering, per-root dependency closure),
  - `$ref` recursion/cycle/fetch limit guards.
- Canonical spec build and endpoint extraction.
- Semantic diff computation.
- Read route selector resolution and endpoint slice responses.
- Outbound notifier signing, retries, and terminal state behavior.
- End-to-end ingest-to-notify flow in `cmd/shiva/webhook_to_notify_integration_test.go`.
- Delete-only incremental integration path in `cmd/shiva/webhook_to_notify_integration_test.go`:
  no artifact persisted, `openapi_changed=true`, `spec_changes` persisted, and outbound emits diff-only event.
- Bootstrap ingest regression guard in `cmd/shiva/webhook_to_notify_integration_test.go`:
  compare has no OpenAPI paths, repository-tree bootstrap still persists artifact/index, and zero-root bootstrap emits no notifications.
- Incremental impact orchestration in `cmd/shiva/revision_processor_incremental_impact_test.go`:
  dependency-intersection impact-only rebuild, unrelated change no rebuild, deleted-root deactivation, fallback discovery for create/rename changes, and per-API permanent-failure isolation (failed API + successful API in one revision).
- Resolver-level incremental behavior in `internal/openapi/resolver_test.go`:
  `ResolveRootOpenAPIAtSHA` strict root validation and fallback discovery (`ResolveDiscoveredRootsAtPaths`) candidate filtering/collapse on changed path inputs.
- Notifier payload identity and dedupe in `internal/notify/notifier_test.go`:
  API-scoped payload contract fields (`api`, `api_revision_id`), per-API event-id identity, and mixed API deliveries in a single repo revision.

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
