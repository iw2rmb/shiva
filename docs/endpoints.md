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
- `GET /v1/specs/{repo}/{openapi|index}.{yaml|json}`
- `GET /v1/specs/{repo}/{selector}/{openapi|index}.{yaml|json}`
- `GET /v1/specs/{repo}/-/{api}/-/{openapi|index}.{yaml|json}`
- `GET /v1/specs/{repo}/-/{api}/-/{selector}/{openapi|index}.{yaml|json}`

### API Inventory
- `GET /v1/specs/{repo}/apis`
- `GET /v1/specs/{repo}/{selector}/apis`

### Endpoint Operation Slice
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{repo}/{path}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{repo}/{selector}/{path}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{repo}/-/{api}/-/{path}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{repo}/-/{api}/-/{selector}/{path}`

Route identity notes:
- `repo` is the GitLab `path_with_namespace`.
- callers must URL-escape `repo` when it contains `/`, for example `group%2Fservice`.

Monorepo `api` is the raw root path, bounded by `/-/{api}/-/` in the URL.
- `api` is decoded and may contain slashes (for example `platform/api`).
- both delimiter segments must be present and in order (`.../-/{api}/-/...`).

Malformed delimiter shapes are rejected as `400`:
- missing closing delimiter `/-/{api}/` (example: `/-/{api}/pets`),
- empty `api` (example: `/-/-/openapi.json`).

Route method is the endpoint method selector.
`/v1/routes/...` applies fallback semantics when selector is present but not found:
- first resolves `/.../{selector}/...`
- if `selector` resolves with `not_found`, resolves again without selector against the same `/{api}/-/...` context.

## Selector Semantics
- selector can only be an 8-character lowercase commit SHA (short SHA prefix).
- no-selector routes resolve to the latest processed `HEAD` on the repo's stored `default_branch`.
- selector operation route is attempted first; if selector is not found it falls through to no-selector operation route.
- spec routes do not fallback: `/v1/specs/.../{selector}/...` resolves selector only; 404 on selector failure.

Route parser behavior:
- non-monorepo: `/v1/specs/{repo}/...` and `/v1/routes/{repo}/...`
- monorepo: `/v1/specs/{repo}/-/{api}/-/...` and `/v1/routes/{repo}/-/{api}/-/...`
- compatibility: no `/-/{api}/-/` means single-API/legacy behavior based on latest processed API-scoped row for revision.

## Path Normalization on Reads
- path parameter is URL-decoded,
- `.json` or `.yaml` suffix on `{path}` is treated as output-format addon,
- if decoded path has no leading slash, `/` is prefixed.

Default operation-slice format is JSON.

## API Listing Response Shape
- `GET /v1/specs/{repo}/apis`
- `GET /v1/specs/{repo}/{selector}/apis`
- HTTP `200` with `application/json` body:
  - `api`: root path of the API spec (`root_path`)
  - `status`: `active` or `deleted`
  - `last_processed_revision`:
    - `api_spec_revision_id`
    - `ingest_event_id` (canonical ingest-event id, backed by `ingest_events.id`)
    - `ingest_event_sha`
    - `ingest_event_branch`
- selector form uses selector-resolved snapshot:
  - the snapshot is derived from `/{selector}/` and list entries include last processed state as of that ingest event
- selector form requires an 8-character lowercase hex selector. Other selector values return `400`.

## Operation Slice Response Shape
Response body shape:
- `{ "paths": { "<path>": { "<method>": <operation-object> } } }`

Status behavior:
- `404` when endpoint key `(method, path)` is absent,
- selector errors map to `404/409` depending on state,
- full-spec routes support `ETag` and `If-None-Match` (`304`).

## References
- Draft CLI behavior on top of current routes: `docs/cli.md`
- Ingestion/build flow: `docs/gitlab.md`
- Runtime setup and selector defaults: `docs/setup.md`
- Webhook-triggered processing context: `docs/webhooks.md`
- Storage schema: `docs/database.md`
- Route tests: `docs/testing.md`
