# Initial OpenAPI Ingestion Bootstrap

Scope: Implement `design/init.md` end-to-end against the current delta-only ingestion pipeline, including bootstrap trigger logic, full-tree discovery, `.shivaignore` filtering, and per-root bootstrap build persistence.

Documentation: `design/init.md`, `design/mono.md`, `design/inc.md`, `docs/gitlab.md`, `docs/database.md`, `internal/gitlab/client.go`, `internal/openapi/resolver.go`, `cmd/shivad/main.go`, `sql/schema/000001_initial.sql`

Legend: [ ] todo, [x] done.

## Scope Boundary
- This roadmap covers full ingestion bootstrap only (`design/init.md`) plus required data foundation from `design/mono.md`.
- Incremental redesign from `design/inc.md` is the next phase after this roadmap is fully implemented and verified end-to-end.
- Keep existing incremental delta flow intact in this roadmap, except integration points needed to avoid regressions.

## Codebase Confirmation
- [x] Confirm current resolver is delta-only and cannot bootstrap existing specs when first processed revision is unrelated — establishes root-cause baseline from code, not assumptions
  - Repository: `shiva`
  - Component: `cmd/shivad`, `internal/openapi`, `internal/gitlab`
  - Scope: `cmd/shivad/main.go` exits early with `openapi_changed=false` when `parent_sha` is empty; resolver path is `ResolveChangedOpenAPI` only; GitLab client supports compare + file fetch only
  - Snippets: `revisionProcessor.Process` currently calls `MarkRevisionProcessed(..., false)` when `parent_sha == ""`; when `parent_sha != ""`, resolver uses `CompareChangedPaths(parent_sha, sha)` and candidate filtering from changed files
  - Tests: Existing resolver tests in `internal/openapi/resolver_test.go` cover changed-path flow only — no full-tree bootstrap coverage

## Data Foundation (Required for Init Trigger/Persistence)
- [x] Add API instance persistence primitives required by init flow — bootstrap trigger and per-root writes depend on durable API identities
  - Repository: `shiva`
  - Component: `sql/schema`, `sql/query`, `internal/store/sqlc`, `internal/store`
  - Scope: Update `sql/schema/000001_initial.sql` with `api_specs`, `api_spec_revisions`, `api_spec_dependencies`; add store methods to count active API specs by repo and upsert per-root bootstrap revision/dependencies
  - Snippets: `CountActiveAPISpecsByRepo(repo_id)`, `UpsertAPISpec(repo_id, root_path)`, `CreateAPISpecRevision(api_spec_id, revision_id, build_status, error)`, `ReplaceAPISpecDependencies(api_spec_revision_id, file_paths[])`
  - Tests: `go test ./internal/store` — assert uniqueness `(repo_id, root_path)`, dependency replacement behavior, and count semantics for active/deleted statuses

- [x] Add repo-level force-rescan state used by bootstrap trigger — init design requires explicit forced rescan path
  - Repository: `shiva`
  - Component: `sql/schema`, `sql/query`, `internal/store`
  - Scope: Add `repos.openapi_force_rescan` boolean (default `false`) and store methods to read/clear this flag during successful bootstrap completion
  - Snippets: `GetRepoBootstrapState(repo_id) -> {active_api_count, force_rescan}`, `ClearRepoForceRescan(repo_id)`
  - Tests: `go test ./internal/store` — force-rescan true triggers bootstrap decision inputs; successful bootstrap clears flag

## GitLab Tree Discovery API
- [x] Extend GitLab client with paginated repository tree listing at specific SHA — bootstrap requires full tree enumeration independent of compare diff
  - Repository: `shiva`
  - Component: `internal/gitlab`
  - Scope: Add `ListRepositoryTree(ctx, projectID, sha, path, recursive)` with internal pagination over `GET /projects/:id/repository/tree`; include typed tree node model and path normalization
  - Snippets: loop pages until `X-Next-Page == ""`, keep `type=file` entries only, return stable `[]TreeEntry{Path, Type}`
  - Tests: `go test ./internal/gitlab` — table-driven tests for multi-page aggregation, 404 mapping to `ErrNotFound`, and query params (`ref`, `recursive`, `path`)

## `.shivaignore` + Ignore Engine
- [x] Implement `.shivaignore` loader/parser and matcher composition — bootstrap filtering must be deterministic and repo-sha specific
  - Repository: `shiva`
  - Component: `internal/openapi` (or dedicated ignore package)
  - Scope: Read optional `/.shivaignore` from target `sha`; parse comments/empty lines/doublestar patterns; reject/ignore negation (`!`) per v1 spec; merge with built-in patterns
  - Snippets: `effectiveIgnores := append(defaultIgnores, fileIgnores...)`; defaults include `**/test*/**`, `**/__tests__/**`, `**/node_modules/**`, `**/vendor/**`
  - Tests: Table-driven parser tests for comments, anchored `/` patterns, malformed line handling, unsupported negation behavior

## Bootstrap Resolver Mode
- [x] Add resolver entrypoint for full-tree discovery and root validation — init mode must discover roots even with unrelated webhook diff
  - Repository: `shiva`
  - Component: `internal/openapi/resolver.go`
  - Scope: Introduce bootstrap method (for example `ResolveRepositoryOpenAPIAtSHA`) that:
    1) lists repository tree,
    2) applies ignore filters,
    3) extension-prefilters `.yaml/.yml/.json`,
    4) performs lightweight top-level sniff,
    5) strict parses and validates top-level `openapi/swagger`,
    6) resolves local `$ref` closure per discovered root
  - Snippets: return per-root resolution objects (`root_path`, `documents`, dependency file list) instead of single candidate set
  - Tests: `go test ./internal/openapi` — fixtures for unrelated change bootstrap, `.shivaignore` exclusion, and zero-root repositories (`openapi_changed=false`)

- [x] Keep existing incremental resolver path intact and explicit — bootstrap adds mode, not hidden behavior changes
  - Repository: `shiva`
  - Component: `internal/openapi`
  - Scope: Preserve `ResolveChangedOpenAPI` for incremental mode and factor shared parsing/ref-resolution helpers to avoid duplicated logic
  - Snippets: shared helpers for `parseDocument`, top-level key checks, and recursive local-ref resolution
  - Tests: Re-run existing resolver suite unchanged; add focused tests to ensure incremental path behavior is preserved

## Revision Processor Orchestration
- [x] Replace current `parent_sha == ""` short-circuit with bootstrap decision flow — current behavior directly causes missed initial discovery
  - Repository: `shiva`
  - Component: `cmd/shivad/main.go` (`revisionProcessor.Process`)
  - Scope: Compute ingestion mode before compare:
    - bootstrap when `active_api_specs == 0` or `openapi_force_rescan == true`,
    - incremental otherwise;
    - bootstrap uses target `sha` even when `parent_sha` is empty
  - Snippets: `mode := decideMode(repoState, job.ParentSha)` then `resolveBootstrap(...)` or `ResolveChangedOpenAPI(...)`
  - Tests: processor tests for mode selection matrix (`parent_sha` empty/non-empty, active specs 0/non-zero, force-rescan flag true/false)

- [x] Persist bootstrap outputs per discovered root and finalize revision status — bootstrap result semantics must match design
  - Repository: `shiva`
  - Component: `cmd/shivad`, `internal/store`
  - Scope: For each discovered root:
    - upsert API instance,
    - create per-API revision row,
    - persist canonical artifact and endpoint index,
    - persist dependency set;
    mark revision processed with `openapi_changed=true` when at least one root built, else `false`
  - Snippets: per-root loop with failure isolation data model in place; if no roots found, skip build loop and mark processed false
  - Tests: integration test: first webhook with unrelated diff still discovers/builds existing root(s); zero-root repo marks processed with `openapi_changed=false`

## Performance and Safety Constraints
- [x] Add bounded concurrency and prefix sniff limits for bootstrap content fetch — full-tree scans need explicit resource control
  - Repository: `shiva`
  - Component: `internal/openapi`, `internal/config`
  - Scope: Add bootstrap worker pool/concurrency cap and max sniff bytes; avoid full file loads before candidate confirmation
  - Snippets: semaphore-based fetch limiter; `sniffReader` reading fixed prefix before full parse fetch
  - Tests: resolver benchmark/focused tests for bounded parallel fetch behavior; ensure deterministic results regardless of fetch order

## Verification and Documentation
- [x] Add end-to-end coverage for initial bootstrap path and regression guards — prevents fallback to delta-only behavior
  - Repository: `shiva`
  - Component: `cmd/shivad`, `internal/openapi`, `internal/gitlab`
  - Scope: Add integration test case where compare diff has no OpenAPI files but repo tree does; assert artifact/index persisted and notifications sent only when `openapi_changed=true`
  - Snippets: fake GitLab client implementing both compare and repository-tree endpoints
  - Tests: `go test ./cmd/shivad ./internal/openapi ./internal/gitlab`

- [x] Update runtime/state docs after implementation — keep `docs/` synchronized with actual behavior in commit series
  - Repository: `shiva`
  - Component: `docs/`
  - Scope: Update `docs/gitlab.md`, `docs/database.md`, `docs/setup.md`, `docs/testing.md` to describe bootstrap trigger, tree API usage, `.shivaignore`, and new persistence entities
  - Snippets: add explicit “bootstrap mode” flow and configuration knobs
  - Tests: docs review checklist: each changed behavior has one authoritative doc location and cross-reference
