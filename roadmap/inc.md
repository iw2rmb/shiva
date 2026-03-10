# Incremental OpenAPI Update Implementation

Scope: Implement `design/inc.md` end-to-end on top of completed bootstrap work from `roadmap/init.md`, including impact-based incremental rebuilds, root deletion handling, and fallback discovery for newly added roots.

Documentation: `design/inc.md`, `design/init.md`, `design/mono.md`, `docs/gitlab.md`, `docs/database.md`, `docs/testing.md`, `internal/openapi/resolver.go`, `cmd/shivad/main.go`, `internal/store`, `sql/schema/000001_initial.sql`

Legend: [ ] todo, [x] done.

## Codebase Confirmation
- [x] Confirm current implementation does not satisfy `design/inc.md` expectations — establishes exact incremental gaps before coding
  - Repository: `shiva`
  - Component: `cmd/shivad`, `internal/openapi`, `internal/store`, `docs`
  - Scope: Last 8 commits keep incremental mode on `ResolveChangedOpenAPI` delta candidates only; no dependency-intersection impact resolver, no root deactivation flow, no targeted fallback discovery when no impacted APIs are found
  - Snippets: `revisionProcessor.Process` incremental branch still calls `ResolveChangedOpenAPI(parent_sha, sha)` directly; resolver only evaluates changed candidate files by include-glob matching
  - Tests: Existing coverage verifies mode selection and bootstrap persistence but does not cover `design/inc.md` impact/deletion/fallback matrix

## Incremental Impact Inputs
- [x] Add store read APIs for active API instances and latest dependency sets — impact resolution requires durable dependency graph lookups
  - Repository: `shiva`
  - Component: `sql/query`, `internal/store/sqlc`, `internal/store`
  - Scope: Add queries/methods to list active `api_specs` for a repo and load each API’s latest dependency file set from `api_spec_revisions` + `api_spec_dependencies`; add API status update operation for root deactivation
  - Snippets: `ListActiveAPISpecsWithLatestDependencies(repo_id)`, `MarkAPISpecDeleted(api_spec_id)`
  - Tests: `go test ./internal/store` — table-driven cases for dependency selection from latest processed rows and status transitions

## Resolver Entrypoints For Incremental Phase
- [x] Add resolver entrypoints for impacted-root rebuild and targeted changed-file discovery — incremental mode needs root-by-root resolution and fallback root creation
  - Repository: `shiva`
  - Component: `internal/openapi`
  - Scope: Introduce resolver methods that:
    1) resolve one known root at `sha` with local `$ref` closure;
    2) run discovery pipeline on an explicit changed-file set (normalize, ignore, extension prefilter, sniff, parse+top-level validation)
  - Snippets: `ResolveRootOpenAPIAtSHA(...)`, `ResolveDiscoveredRootsAtPaths(...)`
  - Tests: `go test ./internal/openapi` — impacted-root dependency closure success/failure and targeted discovery acceptance/rejection cases

## Revision Processor Incremental Orchestration
- [x] Replace delta-only incremental path with dependency-intersection impact resolution — implement core behavior from `design/inc.md`
  - Repository: `shiva`
  - Component: `cmd/shivad/main.go`
  - Scope: For incremental mode:
    - load compare changed paths once;
    - compute impacted APIs via intersection against `{root_path + dependency_paths}`;
    - rebuild only impacted APIs;
    - deactivate roots deleted in compare;
    - if no impacted APIs and changed paths include create/rename candidates, run targeted discovery and create new API instances.
  - Snippets: `resolveImpactedAPIs(...)`, `processImpactedAPI(...)`, `processFallbackDiscovery(...)`
  - Tests: `go test ./cmd/shivad` — impact-only rebuild, unrelated changes no rebuild, deleted root deactivation, and fallback discovery from create/rename changes

## Failure Isolation And Completion Semantics
- [x] Isolate per-API build failures in incremental mode while preserving revision completion behavior — one API failure must not block others
  - Repository: `shiva`
  - Component: `cmd/shivad/main.go`, `internal/store`
  - Scope: Persist per-API revision status (`processing|processed|failed`) for incremental impacted/fallback APIs, continue processing remaining APIs on permanent root-local failures, and fail whole revision only for infrastructure failures
  - Snippets: reusable per-API execution loop with root-local error classification via `isPermanentOpenAPIProcessingError`
  - Tests: `go test ./cmd/shivad` — one impacted API fails with invalid root while another succeeds in same revision

## Documentation Synchronization
- [x] Update runtime and verification docs to match implemented incremental behavior — keep docs authoritative for current ingest flow
  - Repository: `shiva`
  - Component: `docs/gitlab.md`, `docs/database.md`, `docs/testing.md`, `docs/endpoints.md`, `docs/webhooks.md`
  - Scope: Document dependency-intersection incremental flow, fallback discovery trigger, root deletion/deactivation semantics, and added test coverage
  - Snippets: explicit mode flow table + failure-scope notes
  - Tests: docs cross-reference pass (`docs/*` link and scope sanity against implemented code)

## Gap Closure: Deleted Root Delivery
- [x] Ensure deleted-root-only incremental revisions still emit semantic diff notifications when canonical artifact is absent
  - Repository: `shiva`
  - Component: `cmd/shivad/main.go`, `internal/notify/notifier.go`, `cmd/shivad/revision_processor_incremental_impact_test.go`, `cmd/shivad/webhook_to_notify_integration_test.go`, `internal/notify/notifier_test.go`, `docs/gitlab.md`, `docs/webhooks.md`, `docs/testing.md`
  - Scope:
    - set `openapi_changed=true` for incremental revisions with root deactivations even when no root rebuild succeeded,
    - persist semantic diff for deletion-only revisions,
    - make full webhook optional when `spec_artifacts` row is missing and still dispatch `spec.updated.diff`.
  - Tests:
    - `go test ./internal/notify`
    - `go test ./cmd/shivad`
    - `go test ./...`
