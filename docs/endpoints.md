# Endpoints

## Scope
This document describes how canonical OpenAPI specs are indexed, how Shiva serves the shipped query-driven inspect, inventory, and call-planning endpoints on `/v1/*`, and the locked contract for the reserved repo-backed runtime surface on `/gl/*`.

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

## Runtime Transport (`/gl/*`)
Current state:
- the `/gl/*` contract is locked
- runtime handlers are not registered yet
- the shipped HTTP surface remains `/v1/*`

### Route Grammar
- `<method> /gl/<repo-path>/<openapi-path>`
- `<method> /gl/<repo-path>/@<selector>/<openapi-path>`
- supported runtime methods are the OpenAPI method set already indexed at build time:
  - `GET`, `PUT`, `POST`, `DELETE`, `OPTIONS`, `HEAD`, `PATCH`, `TRACE`
- `<selector>` is optional
- v1 selector values are limited to:
  - `latest`
  - an 8-character lowercase hexadecimal Git SHA prefix
- selector values outside that grammar return `400`

### Repo and Snapshot Resolution
- route parsing starts after the `/gl/` prefix
- Shiva tries repo-path candidates longest-first until one existing `(namespace, repo)` pair matches stored repo metadata
- after repo resolution:
  - the next segment is treated as optional `@selector`
  - the remaining suffix is treated as the OpenAPI path
- the OpenAPI path is canonicalized to include a leading `/`
- no selector and `@latest` resolve the latest processed snapshot on the repo default branch
- `@<sha8>` resolves one repo-scoped short SHA prefix
- there is no fallback from selector errors to another snapshot target

### Validation Library Choice
- runtime request and response validation is fixed to `github.com/getkin/kin-openapi/openapi3` plus `github.com/getkin/kin-openapi/openapi3filter`
- Shiva resolves runtime routes dynamically from stored repo snapshots at request time
- static middleware generated for one compiled spec, including `oapi-codegen` Fiber adapters, is not a fit for `/gl/*`
- the runtime surface returns deterministic spec-shaped stub responses and does not proxy upstream traffic

## Query and Call-Planning Endpoints (`/v1/*`)

### Registered Surface
- `GET /v1/spec`
- `GET /v1/operation`
- `POST /v1/call`
- `GET /v1/apis`
- `GET /v1/operations`
- `GET /v1/repos`
- `GET /v1/catalog/status`

Legacy path-segment endpoints were removed:
- `/v1/specs/...`
- `/v1/routes/...`

### Shared Query Parameters
- `namespace`
  - required on `/v1/spec`, `/v1/operation`, `/v1/apis`, `/v1/operations`, and `/v1/catalog/status`
  - value is the GitLab namespace path prefix before the final slash
- `repo`
  - required on `/v1/spec`, `/v1/operation`, `/v1/apis`, `/v1/operations`, and `/v1/catalog/status`
  - value is the GitLab project slug
- `api`
  - optional on `/v1/spec`, `/v1/operation`, and `/v1/operations`
  - value is the raw `api_specs.root_path`
- `revision_id`
  - optional positive integer ingest-event id
- `sha`
  - optional 8-character lowercase hex SHA prefix
- exactly one of `revision_id`, `sha`, or neither is allowed
- `neither` means latest processed OpenAPI snapshot on `repos.default_branch`
- invalid query combinations return `400`

### Snapshot Resolution
- repo lookup uses `(repos.namespace, repos.repo)`
- `revision_id` resolves the exact ingest-event row and rejects repo mismatches
- `sha` resolves one repo-scoped short SHA prefix
- no selector resolves the latest processed OpenAPI snapshot on the repo default branch
- unresolved snapshots return `404`
- unprocessed head or selector targets return `409`
- there is no selector fallback behavior on query endpoints

## Endpoint Contracts

### `GET /v1/spec`
- supported query parameters:
  - `namespace`
  - `repo`
  - optional `api`
  - optional `revision_id` or `sha`
  - optional `format=json|yaml` (default `json`)
- response body is the canonical spec body for one resolved API snapshot
- `ETag` and `If-None-Match` are supported
- omitting `api` is valid only when the selected repo snapshot resolves to exactly one API snapshot
- ambiguous no-`api` resolution returns `409` with candidate API rows

### `GET /v1/operation`
- supported query parameters:
  - `namespace`
  - `repo`
  - optional `api`
  - optional `revision_id` or `sha`
  - either:
    - `operation_id`
    - or `method` plus `path`
- `operation_id` is mutually exclusive with `method` and `path`
- `method` is normalized to lowercase and must be one of:
  - `get`, `post`, `put`, `patch`, `delete`, `head`, `options`, `trace`
- `path` is normalized to include a leading `/`
- response body is the raw canonical operation object
- ambiguous cross-API or duplicate-operation matches return `409` with candidate rows

### `POST /v1/call`
- request body is one JSON object using the shared CLI request-envelope shape
- accepted request fields:
  - `kind`
  - `namespace`
  - `repo`
  - optional `api`
  - optional `revision_id` or `sha`
  - optional `target`
  - either:
    - `operation_id`
    - or `method` plus `path`
  - optional `path_params`
  - optional `query_params`
  - optional `headers`
  - optional `json`
  - optional `body`
  - optional `dry_run`
- input validation matches the query read surface:
  - `namespace` and `repo` are required
  - `kind`, when present, must be `call`
  - `target`, when present, must be `shiva`
  - `operation_id` is mutually exclusive with `method` and `path`
  - `revision_id` and `sha` are mutually exclusive on input
  - `json` and `body` are mutually exclusive
- the handler resolves the target operation through the same snapshot and operation-selection rules used by `GET /v1/operation`
- response body is a normalized call plan:
  - `request`
    - explicit `namespace`, explicit `repo`, resolved `api`, resolved `revision_id`, resolved `sha`, resolved `method`, resolved `path`, chosen `target`, optional resolved `operation_id`, and request-input fields
  - `dispatch`
    - `mode`
    - `dry_run`
    - `network`
- `target` defaults to `shiva` when omitted
- the current endpoint is planning-only: it does not dispatch an outbound call and always reports `dispatch.network=false`
- ambiguous resolution returns `409` with operation candidate rows

### `GET /v1/apis`
- supported query parameters:
  - `namespace`
  - `repo`
  - optional `revision_id` or `sha`
- response body is an array of API snapshot rows
- rows include current API status plus resolved snapshot metadata when present:
  - `api`
  - `status`
  - `display_name`
  - `has_snapshot`
  - `api_spec_revision_id`
  - `ingest_event_id`
  - `ingest_event_sha`
  - `ingest_event_branch`
  - `spec_etag`
  - `spec_size_bytes`
  - `operation_count`

### `GET /v1/operations`
- supported query parameters:
  - `namespace`
  - `repo`
  - optional `api`
  - optional `revision_id` or `sha`
- response body is an array of operation inventory rows
- rows include:
  - `api`
  - `status`
  - `api_spec_revision_id`
  - `ingest_event_id`
  - `ingest_event_sha`
  - `ingest_event_branch`
  - `method`
  - `path`
  - `operation_id`
  - `summary`
  - `deprecated`
  - `operation` (raw canonical operation object)
- explicit `api` selection is validated before listing so `404` is reserved for missing API snapshots, not empty inventories

### `GET /v1/repos`
- takes no repo/snapshot selector parameters
- response body is an array of repo catalog rows
- rows include:
  - `namespace`
  - `repo`
  - `gitlab_project_id`
  - `default_branch`
  - `openapi_force_rescan`
  - `active_api_count`
  - `head_revision`
  - `snapshot_revision`

### `GET /v1/catalog/status`
- supported query parameters:
  - `namespace`
  - `repo`
- returns the current default-branch freshness row for that repo
- payload shape matches one `/v1/repos` row
- this endpoint does not accept `api`, `revision_id`, or `sha`

## Error Behavior
- `400`
  - malformed or unsupported query combinations
  - malformed call-envelope bodies
- `404`
  - repo, snapshot, API snapshot, spec, or operation not found
- `409`
  - unprocessed snapshot targets
  - ambiguous no-`api` spec resolution
  - ambiguous operation resolution
  - ambiguous call resolution
- `503`
  - database is not configured
- `500`
  - unexpected server failures

## References
- CLI behavior on top of the current transport: `docs/cli.md`
- Ingestion/build flow: `docs/gitlab.md`
- Runtime setup and selector defaults: `docs/setup.md`
- Webhook-triggered processing context: `docs/webhooks.md`
- Storage schema: `docs/database.md`
- Endpoint tests: `docs/testing.md`
