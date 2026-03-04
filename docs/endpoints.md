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

Endpoints are sorted by `(method, path)` and persisted to `endpoint_index` with unique key `(revision_id, method, path)`.

## Read Routes

### Full Spec
- `GET /{tenant}/{repo}.{json|yaml}`
- `GET /{tenant}/{repo}/{selector}.{json|yaml}`

### Endpoint Operation Slice
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /{tenant}/{repo}/{path}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /{tenant}/{repo}/{selector}/{path}`

Route method is the endpoint method selector.

## Selector Semantics
- selector can be commit SHA, branch, or `latest`.
- no-selector routes resolve to latest processed revision on `main`.
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
