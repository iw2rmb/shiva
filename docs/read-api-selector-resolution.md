# Read API Selector Resolution (Item 10)

## Status
- Implemented: selector resolution and artifact-backed read routes.
- Current route contract:
  - `GET /{tenant}/{repo}.{json|yaml}`
  - `GET /{tenant}/{repo}/{selector}.{json|yaml}`
  - `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /{tenant}/{repo}/{path}`
  - `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /{tenant}/{repo}/{selector}/{path}`

## Components
- HTTP routes and handlers:
  - file: `internal/http/server.go`
  - file: `internal/http/read_routes.go`
- Selector resolver and typed selector errors:
  - file: `internal/store/read_selector.go`
- Endpoint lookup/index reads:
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
  - when head is processed, artifact routes resolve to latest `processed + openapi_changed=true` revision for that branch.
- `latest`:
  - same branch behavior, using repo `default_branch`.
- no selector:
  - uses latest processed revision on `main`.

## Path Slice Semantics
- `/{tenant}/{repo}.{json|yaml}` and `/{tenant}/{repo}/{selector}.{json|yaml}`:
  - full canonical spec for resolved revision.
- `/{tenant}/{repo}/{path}` and `/{tenant}/{repo}/{selector}/{path}`:
  - operation-level spec slice (JSON by default).
  - endpoint method is derived from HTTP request method.
  - supports `.json` / `.yaml` suffix on `{path}` for explicit output format.
  - selector route is evaluated first; when selector is not found, route falls back to no-selector path resolution.

## HTTP Response Semantics
- Artifact-backed selector misses return `404`:
  - response body error: `selector has no processed artifact`.
- Selector conflict on unprocessed revision returns `409`:
  - response body error: `selector points to unprocessed revision`.
- Spec responses include ETag:
  - `ETag` set from `spec_artifacts.etag`.
  - `If-None-Match` supported on full-spec routes (returns `304`).

## Tests
- Store selector tests:
  - `internal/store/read_selector_test.go`
- HTTP route tests:
  - `internal/http/read_routes_test.go`
  - covers selector/no-selector resolution, route precedence, `404/409`, and ETag `304`.

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Canonical artifact build/persistence: `docs/canonical-spec-build-persistence.md`
- Semantic diff engine: `docs/semantic-diff-engine.md`
- Outbound notifications: `docs/outbound-webhook-notifications.md`
- Hardening controls: `docs/hardening-observability-security-controls.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
