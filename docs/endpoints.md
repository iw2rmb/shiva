# Endpoints

## Scope
This document describes how endpoint paths are built from canonical OpenAPI specs and how endpoint slices are served by read routes.

## Build-Time Endpoint Extraction
Canonical build:
- picks canonical root from resolver candidates (sorted, first existing candidate),
- expands local `$ref` graph into one canonical document,
- renders canonical `spec_json`, `spec_yaml`, and `etag`.

Endpoint extraction from canonical document:
- reads top-level `paths` object,
- accepts methods: `get`, `put`, `post`, `delete`, `options`, `head`, `patch`, `trace`,
- for each valid operation object, stores:
  - `method` (lowercase),
  - `path` (exact OpenAPI path key),
  - `operation_id`, `summary`, `deprecated`,
  - `raw_json` (canonical operation JSON).

Endpoints are sorted by `(method, path)` and persisted to `endpoint_index` with unique key `(api_spec_revision_id, method, path)`.

Persistence is API-revision scoped: `PersistCanonicalSpec` upserts `spec_artifacts` and replaces the full `endpoint_index` for an API revision.

## Read Routes

### Full Spec
- `GET /v1/specs/{tenant}/{repo}/{openapi|index}.{yaml|json}`
- `GET /v1/specs/{tenant}/{repo}/{selector}/{openapi|index}.{yaml|json}`
- `GET /v1/specs/{tenant}/{repo}/-/{api}/-/{openapi|index}.{yaml|json}`
- `GET /v1/specs/{tenant}/{repo}/-/{api}/-/{selector}/{openapi|index}.{yaml|json}`

### Endpoint Operation Slice
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{tenant}/{repo}/{path}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{tenant}/{repo}/{selector}/{path}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{tenant}/{repo}/-/{api}/-/{path}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{tenant}/{repo}/-/{api}/-/{selector}/{path}`

Monorepo `api` is the raw root path, bounded by `/-/{api}/-/` in the URL.

Malformed delimiter shapes are rejected as `400`:
- missing closing delimiter `/-/{api}/` (example: `/-/{api}/pets`),
- empty `api` (example: `/-/-/openapi.json`).

Route method is the endpoint method selector.

## Selector Semantics
- selector can only be an 8-character lowercase commit SHA (short SHA prefix).
- no-selector routes resolve to latest processed `HEAD` on `main`.
- selector operation route is attempted first; if selector is not found it falls through to no-selector operation route.

## Path Normalization on Reads
- path parameter is URL-decoded,
- `.json` or `.yaml` suffix on `{path}` is treated as output-format addon,
- if decoded path has no leading slash, `/` is prefixed.

Default operation-slice format is JSON.

## Operation Slice Response Shape
Response body shape:
- `{ "paths": { "<path>": { "<method>": <operation-object> } } }`

Status behavior:
- `404` when endpoint key `(method, path)` is absent,
- selector errors map to `404/409` depending on state,
- full-spec routes support `ETag` and `If-None-Match` (`304`).

## References
- Ingestion/build flow: `docs/gitlab.md`
- Runtime setup and selector defaults: `docs/setup.md`
- Webhook-triggered processing context: `docs/webhooks.md`
- Storage schema: `docs/database.md`
- Route tests: `docs/testing.md`
