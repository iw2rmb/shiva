# Monorepo Read/Storage Refactor Implementation

Scope: Implement `design/mono.md` end-to-end for multi-OpenAPI repositories, including per-API persistence, per-API processing/diff/notify, and `/v1/specs` + `/v1/routes` read contracts with `/-/{api}/-/` delimiters and optional short-SHA selector.

Documentation: `design/mono.md`, `design/init.md`, `design/inc.md`, `docs/database.md`, `docs/gitlab.md`, `docs/endpoints.md`, `docs/webhooks.md`, `docs/testing.md`, `sql/schema/000001_initial.sql`, `internal/store`, `cmd/shiva/main.go`, `internal/http`, `internal/notify`

Legend: [ ] todo, [x] done.

## Baseline Confirmation
- [x] Confirm current codebase gaps against `design/mono.md` before refactor — lock exact mismatch set and prevent scope drift
  - Repository: `shiva`
  - Component: `internal/store`, `cmd/shiva`, `internal/http`, `internal/notify`, `docs`
  - Scope: verify current revision-scoped artifact/index/change keys, current read route shapes, current selector semantics, and notification payload identity
  - Locked mismatch notes:
    - Route shape: current read routes are registered in `internal/http/server.go`/`internal/http/read_routes.go` as legacy `/:tenant/:repo` and `/:tenant/:repo/:selector/*` without `/v1/specs`, `/v1/routes`, or `/-/{api}/-/` API-delimited segments.
    - Selector semantics: selector is optional legacy mode; accepted selectors include 40-char SHA, `latest`, and arbitrary branch names, with no-selector defaulting to `main`; design requires optional short SHA only and optional omitted selector defaulting to `HEAD` on `main`.
    - Store key scopes: `spec_artifacts`, `endpoint_index`, and `spec_changes` are still revision-scoped via `revision_id`/`to_revision_id` in schema, SQL, and store calls (`PersistCanonicalSpec`, `PersistSpecChange`, `GetSpecArtifactByRevisionID`, `ListEndpointIndexByRevision`, etc.).
    - Notification identity: outbound events are revision-scoped and include `revision_id` only; payload/key identity does not include `api` root, `api_spec_id`, or API-scoped revision id in `internal/notify` or `cmd/shiva` notification flow.
  - Snippets: map current paths in `internal/http/routes.go` and selector flow in `internal/store/read_selector.go`
  - Tests: `go test ./...` baseline must pass before refactor

## Schema and Query Refactor
- [x] Refactor persistence keys from revision-scoped to API-scoped entities — monorepo correctness requires per-API history boundaries
  - Repository: `shiva`
  - Component: `sql/schema/000001_initial.sql`, `sql/query`, `internal/store/sqlc`
  - Scope:
    - keep `api_specs`, `api_spec_revisions`, `api_spec_dependencies` as first-class entities
    - change `spec_artifacts` to key by `api_spec_revision_id`
    - change `endpoint_index` to key by `api_spec_revision_id`
    - change `spec_changes` to key by `api_spec_id` and from/to `api_spec_revision_id`
    - extend delivery identity fields to include `api_spec_id`
  - Snippets: update CREATE statements and sqlc query contracts in-place (no ALTER/data-migration plan)
  - Tests: `go test ./internal/store` plus sqlc regeneration checks

## Store Layer Contracts
- [x] Introduce API-scoped store APIs for build/read/diff/notify loops — remove revision-level single-spec assumptions
  - Repository: `shiva`
  - Component: `internal/store`
  - Scope:
    - add typed read/write methods for `api_spec_revision_id` artifacts and endpoint index
    - add typed read/write methods for `api_spec_id` change history
    - expose API listing read model: `api`, `status`, `last processed api revision`
    - keep branch persisted in revision metadata, but read selector contract independent of branch selector segment
  - Snippets: replace `GetSpecArtifactByRevisionID`/`GetSpecChangeByToRevision` style calls with API-scoped variants
  - Tests: table-driven store tests for per-API isolation and listing semantics

## Revision Processor Per-API Execution
- [ ] Move build/persist/diff/notify orchestration to per-API loops — one API failure must not block others
  - Repository: `shiva`
  - Component: `cmd/shiva/main.go`
  - Scope:
    - process impacted APIs independently in incremental mode
    - persist artifacts/index per `api_spec_revision_id`
    - compute/persist spec changes per `api_spec_id`
    - keep repo revision `openapi_changed=true` when any API changed
    - preserve infra-failure behavior as revision-scoped failure
  - Snippets: split current revision-scoped persist/diff/notify calls into API-scoped units keyed by `api_spec_id` and `api_spec_revision_id`
  - Tests: `go test ./cmd/shiva` with multi-root fixtures proving independent success/failure and independent diff state

## HTTP Route Namespace and Parser
- [ ] Implement `/v1/specs` and `/v1/routes` route families with delimiter-safe monorepo API segment — eliminate ambiguity between API path and endpoint path
  - Repository: `shiva`
  - Component: `internal/http`
  - Scope:
    - serve Shiva service routes under `/v1/...`
    - add specs routes:
      - `/v1/specs/{tenant}/{repo}/...`
      - `/v1/specs/{tenant}/{repo}/-/{api}/-/...`
    - add route-proxy routes:
      - `/v1/routes/{tenant}/{repo}/...`
      - `/v1/routes/{tenant}/{repo}/-/{api}/-/...`
    - enforce monorepo API requirement and parse `api` as real path bounded by `/-/`
  - Snippets: central parser for `/-/{api}/-/` extraction reused by specs/routes handlers
  - Tests: HTTP route tests for delimiter handling, ambiguous path rejection, and mono/single-spec route separation

## Selector Simplification
- [ ] Replace branch/latest selector routing with optional short-SHA selector — stabilize external contract on `HEAD` default
  - Repository: `shiva`
  - Component: `internal/http`, `internal/store/read_selector.go`
  - Scope:
    - accept optional selector only as 8-char lowercase hex
    - selector omitted => resolve latest processed `HEAD` on `main`
    - keep branch in DB schema/state; drop branch selector from route contract
  - Snippets: normalize selector input and resolve short SHA against repo revision history
  - Tests: selector validation/resolution tests for invalid length, invalid charset, not-found SHA, and default head behavior

## API Listing Routes
- [ ] Implement `/v1/specs/.../apis` listing for single-spec and monorepo repos — provide explicit API inventory view
  - Repository: `shiva`
  - Component: `internal/http`, `internal/store`
  - Scope:
    - add `/v1/specs/{tenant}/{repo}/apis`
    - add `/v1/specs/{tenant}/{repo}/{selector}/apis`
    - include API root path, status, and last processed API revision metadata in response
  - Snippets: stable response DTO for API listing with selector-resolved snapshot
  - Tests: handler tests for selector/no-selector listing, deleted API visibility, and deterministic ordering

## Notification Contract Refactor
- [ ] Move outbound notifications to API-scoped payload identity — subscribers must identify changed API instance directly
  - Repository: `shiva`
  - Component: `internal/notify`, `cmd/shiva/main.go`
  - Scope:
    - include `api` root path and `api_revision_id` in payload
    - key delivery attempt identity with `api_spec_id`
    - emit full/diff events per API instance
  - Snippets: event envelope and id generation include `api_spec_id` and API-scoped revision id
  - Tests: notifier tests for per-API dedupe keying and mixed API deliveries in one repo revision

## Documentation Sync
- [ ] Update runtime and contract docs to match monorepo implementation — keep docs authoritative after breaking refactor
  - Repository: `shiva`
  - Component: `docs/database.md`, `docs/gitlab.md`, `docs/endpoints.md`, `docs/webhooks.md`, `docs/testing.md`
  - Scope: replace revision-scoped artifact/index/change narratives with API-scoped behavior and document `/v1/specs|routes` contracts with `/-/{api}/-/` delimiters and short-SHA selector
  - Snippets: route tables and selector rules with concrete path examples
  - Tests: doc cross-reference pass + full suite (`go test ./...`)
