# Testing

## Scope
This document describes the current test layout and practical test commands.

## Main Command
- Run full suite:
  - `go test ./...`

Current baseline should be validated by running `go test ./...`.

## Focused Commands
- HTTP query/runtime endpoints and webhook handlers:
  - `go test ./internal/http`
- Runtime endpoint contract and stub-response tests only:
  - `go test ./internal/http -run Runtime`
- End-to-end CLI contract and entrypoint wiring:
  - `go test ./cmd/shiva`
- Shared CLI request-envelope, call-planning, and executor packages:
  - `go test ./internal/cli/request ./internal/cli/executor`
- CLI parser, request transport, and command entrypoint:
  - `go test ./internal/cli/... ./cmd/shiva/...`
- OpenAPI resolver/build/diff:
  - `go test ./internal/openapi`
- Store + selector behavior:
  - `go test ./internal/store`
- Worker behavior:
  - `go test ./internal/worker`
- End-to-end pipeline integration test package:
  - `go test ./cmd/shivad`
- Documentation cross-reference check:
  - `~/@iw2rmb/auto/scripts/check_docs_links.sh`

## Coverage Areas
- Config parsing and defaults.
- CLI shorthand parsing, request-envelope normalization, query-transport dispatch, health command wiring, and static completion generation.
- XDG-backed CLI config loading, source-profile selection, target source-profile overrides, catalog refresh policy, and offline cache reuse.
- GitLab API client behavior.
- Startup schema migration bootstrap and checksum validation.
- Vacuum schema bootstrap seeding and store-level issue replacement / revision vacuum state transitions.
- Canonical vacuum runner normalization, including deterministic issue ordering and normalized lint-failure messages.
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
- GitLab CI validation route registration, request-contract validation, Shiva JSON formatting, and GitLab Code Quality response formatting.
- GitLab CI validation service no-op compare behavior, impacted-root revalidation, fallback discovery, and repository discovery when `parent_sha` is absent.
- Source-layout vacuum execution and temp-workspace path remapping back to repo-relative file paths.
- Revision-processor vacuum stage success, zero-issue success, and normalized failure persistence behavior.
- Runtime route parsing, repo/snapshot resolution, ambiguity handling, request validation, and spec-shaped stub response generation on `/gl/*`.
- Shared call-envelope normalization, Shiva call-plan generation, direct-target planning, and dispatch behavior.
- CLI snapshot-store resolution, repo/API/operation inventories, candidate-preserving operation lookup, and catalog freshness mapping.
- CLI request-input parsing, selector-driven `ls` rendering, and `batch` NDJSON execution.
- Endpoint contract tests for:
  - `/internal/gitlab/ci/validate` request validation, service-unconfigured behavior, and both response formats,
  - `/gl/*` repo-path parsing, selector resolution, method/path normalization, ambiguity handling, request validation, fallback `400` behavior, and deterministic stub responses,
  - shared `repo`/`api`/`revision_id`/`sha` query validation,
  - `/v1/spec` format and `ETag` behavior,
  - `/v1/operation` operation-id vs method/path resolution rules,
  - `/v1/call` request-envelope validation, ambiguity reporting, and resolved planning payloads,
  - `/v1/apis`, `/v1/operations`, `/v1/repos`, and `/v1/catalog/status` response shapes,
  - removal of legacy `/v1/specs` and `/v1/routes` read surfaces.
- Internal CI validation service tests in `internal/http/gitlab_ci_validator_service_test.go`:
  no-spec compare responses, impacted-root validation, fallback discovery, and repository discovery without `parent_sha`.
- Source-layout vacuum tests in `internal/openapi/lint/source_test.go`:
  repo-relative file remapping from temp workspaces and input validation.
- Canonical vacuum and processor vacuum-stage tests in `internal/openapi/lint/vacuum_test.go` and `cmd/shivad/revision_processor_vacuum_test.go`:
  deterministic issue normalization, failure normalization, and final revision-state persistence.
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
- `docs/cli.md`: shipped CLI surface, output, and current limits.
- `docs/database.md`: API-scoped artifact/index/change and listing behavior.
- `docs/endpoints.md`: `/v1/*` query/call-planning contract, `/gl/*` runtime contract, selector semantics, and response shapes.
- `docs/webhooks.md`: API-scoped notification identity and payload.
- `docs/gitlab.md`: per-API changed revision flow and notification emission conditions.

## DB/Query Change Validation
When SQL schema/query files change:
1. Regenerate sqlc code (`sqlc generate`).
2. Run focused store tests (`go test ./internal/store`).
3. Run full suite (`go test ./...`).

## References
- CLI behavior: `docs/cli.md`
- Runtime/setup details: `docs/setup.md`
- Ingest behavior under test: `docs/gitlab.md`
- Endpoint route behavior under test: `docs/endpoints.md`
- Webhook contracts under test: `docs/webhooks.md`
- Schema/query generation workflow: `docs/database.md`
