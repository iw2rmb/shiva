# Read API Selector Resolution (Item 10)

## Status
- Implemented: selector resolution and artifact-backed read routes.
- Scope completed:
  - `GET /{tenant}/{repo}/{selector}/spec.json`
  - `GET /{tenant}/{repo}/{selector}/spec.yaml`
  - `GET /{tenant}/{repo}/{selector}/endpoints`
  - `GET /{tenant}/{repo}/{selector}/endpoints/{method}/{path}`
  - `GET /{tenant}/{repo}/endpoints` (no-selector route on `main`)
- Explicitly out of scope for this item:
  - hardening controls (item 11),
  - broader release-readiness coverage (item 12).

## Components
- HTTP routes and handlers:
  - file: `internal/http/server.go`
  - file: `internal/http/read_routes.go`
- Selector resolver and typed selector errors:
  - file: `internal/store/read_selector.go`
- Endpoint detail lookup:
  - file: `internal/store/endpoint_index.go`
- Branch-head query support:
  - file: `sql/query/revisions.sql`

## Selector Semantics
- `sha`:
  - exact `repo_id + sha` lookup.
  - `409` when revision exists but `status != processed`.
  - `404` when processed revision has no OpenAPI artifact (`openapi_changed != true` or missing artifact row).
- `branch`:
  - branch head is loaded first (latest revision by `created_at, id`).
  - `409` when branch head is unprocessed.
  - when head is processed, artifact endpoints resolve to latest `processed + openapi_changed=true` revision for that branch.
- `latest`:
  - same branch behavior, using repo `default_branch`.
- no selector (`/{tenant}/{repo}/endpoints`):
  - same branch behavior, fixed to `main`.

## HTTP Response Semantics
- Artifact-backed selector misses return `404`:
  - response body error: `selector has no processed artifact`.
- Selector conflict on unprocessed revision returns `409`:
  - response body error: `selector points to unprocessed revision`.
- Spec responses include ETag:
  - `ETag` set from `spec_artifacts.etag`.
  - `If-None-Match` supported for `spec.json` and `spec.yaml` (returns `304`).

## Endpoint Route Behavior
- Endpoint list route returns indexed endpoint rows for resolved artifact revision.
- Endpoint detail route normalizes:
  - `method` to lowercase,
  - endpoint `path` to leading-slash form.
- Endpoint detail returns `404` when `(method, path)` is absent in `endpoint_index`.

## Tests
- Store selector tests:
  - `internal/store/read_selector_test.go`
- HTTP read route tests:
  - `internal/http/read_routes_test.go`
  - covers `sha`/`branch`/`latest`/no-selector routes, `404`/`409`, and ETag `304`.

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Canonical artifact build/persistence: `docs/canonical-spec-build-persistence.md`
- Semantic diff engine: `docs/semantic-diff-engine.md`
- Outbound notifications: `docs/outbound-webhook-notifications.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
