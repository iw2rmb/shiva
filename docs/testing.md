# Testing

## Scope
This document describes the current test layout and practical test commands.

## Main Command
- Run full suite:
  - `go test ./...`

Current baseline should be validated by running `go test ./...`.

## Focused Commands
- HTTP query endpoints and webhook handlers:
  - `go test ./internal/http`
- Shared CLI request-envelope and call-planning packages:
  - `go test ./internal/cli/request ./internal/cli/executor`
- Draft CLI parser, service logic, and completion generation:
  - `go test ./internal/cli ./cmd/shiva`
- OpenAPI resolver/build/diff:
  - `go test ./internal/openapi`
- Store + selector behavior:
  - `go test ./internal/store`
- Worker behavior:
  - `go test ./internal/worker`
- End-to-end pipeline integration test package:
  - `go test ./cmd/shivad`

## Coverage Areas
- Config parsing and defaults.
- Draft CLI selector parsing, single-active-API resolution, client-side `operationId` lookup, and static completion generation.
- GitLab API client behavior.
- Startup schema migration bootstrap and checksum validation.
- Startup indexing orchestration and checkpoint resume behavior.
- Inbound webhook validation + ingest persistence behavior.
- Worker retry and permanent-failure handling.
- OpenAPI resolver:
  - incremental path (`compare`, candidate filtering, strict top-level validation, duplicate candidate collapse),
  - incremental entrypoints for known-root rebuild (`ResolveRootOpenAPIAtSHA`) and explicit-path targeted discovery (`ResolveDiscoveredRootsAtPaths`),
  - bootstrap path (`repository/tree` discovery, `.shivaignore` filtering, bounded candidate fetch concurrency, deterministic root ordering, per-root dependency closure),
  - `$ref` recursion/cycle/fetch limit guards.
- Canonical spec build and endpoint extraction.
- Semantic diff computation.
- Query endpoint validation, snapshot resolution, ambiguity reporting, and catalog payload mapping.
- Shared call-envelope normalization and Shiva call-plan generation.
- CLI snapshot-store resolution, repo/API/operation inventories, candidate-preserving operation lookup, and catalog freshness mapping.
- Endpoint contract tests for:
  - shared `repo`/`api`/`revision_id`/`sha` query validation,
  - `/v1/spec` format and `ETag` behavior,
  - `/v1/operation` operation-id vs method/path resolution rules,
  - `/v1/call` request-envelope validation, ambiguity reporting, and resolved planning payloads,
  - `/v1/apis`, `/v1/operations`, `/v1/repos`, and `/v1/catalog/status` response shapes,
  - removal of legacy `/v1/specs` and `/v1/routes` read surfaces.
- Outbound notifier signing, retries, and terminal state behavior.
- End-to-end ingest-to-notify flow in `cmd/shivad/webhook_to_notify_integration_test.go`.
- Startup queue seeding in `cmd/shivad/startup_indexer_test.go`:
  zero-checkpoint startup seeding, checkpoint resume via `id_after`, personal-project skip behavior, skip rules for missing default branch/head, checkpoint advancement, and failure behavior for checkpoint load / project discovery / enqueue.
- Delete-only incremental integration path in `cmd/shivad/webhook_to_notify_integration_test.go`:
  no artifact persisted, `openapi_changed=true`, `spec_changes` persisted, and outbound emits diff-only event.
- Bootstrap ingest regression guard in `cmd/shivad/webhook_to_notify_integration_test.go`:
  compare has no OpenAPI paths, repository-tree bootstrap still persists artifact/index, and zero-root bootstrap emits no notifications.
- Incremental impact orchestration in `cmd/shivad/revision_processor_incremental_impact_test.go`:
  dependency-intersection impact-only rebuild, unrelated change no rebuild, deleted-root deactivation, fallback discovery for create/rename changes, and per-API permanent-failure isolation (failed API + successful API in one revision).
- Resolver-level incremental behavior in `internal/openapi/resolver_test.go`:
  `ResolveRootOpenAPIAtSHA` strict root validation and fallback discovery (`ResolveDiscoveredRootsAtPaths`) candidate filtering/collapse on changed path inputs.
- Notifier payload identity and dedupe in `internal/notify/notifier_test.go`:
  API-scoped payload contract fields (`api`, `api_revision_id`), per-API event-id identity, and mixed API deliveries in a single repo revision.

## Cross-Checks
- `docs/cli.md`: draft CLI surface, output, and current limits.
- `docs/database.md`: API-scoped artifact/index/change and listing behavior.
- `docs/endpoints.md`: query endpoint contract, selector semantics, and response shapes.
- `docs/webhooks.md`: API-scoped notification identity and payload.
- `docs/gitlab.md`: per-API changed revision flow and notification emission conditions.

## DB/Query Change Validation
When SQL schema/query files change:
1. Regenerate sqlc code (`sqlc generate`).
2. Run focused store tests (`go test ./internal/store`).
3. Run full suite (`go test ./...`).

## References
- Draft CLI behavior: `docs/cli.md`
- Runtime/setup details: `docs/setup.md`
- Ingest behavior under test: `docs/gitlab.md`
- Endpoint route behavior under test: `docs/endpoints.md`
- Webhook contracts under test: `docs/webhooks.md`
- Schema/query generation workflow: `docs/database.md`
