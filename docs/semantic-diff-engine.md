# Semantic Diff Engine (Item 8)

## Status
- Implemented: semantic diff between previous processed OpenAPI revision and current revision.
- Scope completed:
  - compare previous processed OpenAPI revision vs current revision on the same branch,
  - classify endpoint-level `added`, `removed`, `changed`,
  - detect parameter changes (`added`, `removed`, `changed` by parameter key),
  - detect schema changes for parameter/request/response schema locations,
  - persist structured JSON into `spec_changes.change_json`.
- Explicitly out of scope for this item:
  - outbound notifications (item 9),
  - read HTTP routes/selectors (item 10).

## Components
- Diff engine:
  - file: `internal/openapi/diff.go`
  - entrypoint: `ComputeSemanticDiff(previous, current)`.
  - inputs are endpoint snapshots (`method`, `path`, `raw_json`) from item-7 endpoint index artifacts.
- Processor integration:
  - file: `cmd/shiva/main.go`
  - flow for `OpenAPIChanged=true` revisions:
    1. persist canonical spec + endpoint index (item 7),
    2. load previous processed OpenAPI revision on same branch,
    3. load endpoint index snapshots for previous and current revisions,
    4. compute semantic diff and persist to `spec_changes`,
    5. mark revision processed.
- Store support:
  - files:
    - `internal/store/revisions.go`
    - `internal/store/endpoint_index.go`
    - `internal/store/spec_changes.go`
  - query updates:
    - `sql/query/revisions.sql`: previous processed OpenAPI revision lookup excluding current revision id,
    - `sql/query/spec_changes.sql`: upsert by `to_revision_id` for retry-safe idempotency.

## `spec_changes.change_json` Shape
- Root object:
  - `version` (currently `1`),
  - `endpoints.added[]` => `{method, path}`,
  - `endpoints.removed[]` => `{method, path}`,
  - `endpoints.changed[]`:
    - `method`, `path`,
    - `change_types` (`parameters`, `schemas`, or `operation`),
    - `parameters.added[]|removed[]|changed[]` => parameter keys (for example `query:limit`),
    - `schemas.added[]|removed[]|changed[]` => schema location keys (for example `responses.200.content.application/json.schema`),
  - `summary`:
    - `added_endpoints`,
    - `removed_endpoints`,
    - `changed_endpoints`,
    - `parameter_changes`,
    - `schema_changes`.
- Determinism:
  - endpoints are sorted by `(method, path)`,
  - nested key lists are sorted lexicographically,
  - persisted JSON is canonicalized by unmarshal+marshal in store normalization.

## Baseline Selection Rules
- Previous baseline is selected from `revisions` by:
  - same `repo_id`,
  - same `branch`,
  - `status='processed'`,
  - `openapi_changed=true`,
  - `id <> current_revision_id`,
  - newest by `(processed_at DESC, id DESC)`.
- If no baseline exists:
  - `from_revision_id` is persisted as `NULL`,
  - previous endpoint set is treated as empty.

## Tests
- `internal/openapi/diff_test.go`
  - `TestComputeSemanticDiff` (table-driven)
- `internal/store/spec_changes_test.go`
  - `TestPersistSpecChange`
  - `TestNormalizePersistSpecChangeInput`

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Worker processing: `docs/ingest-worker-processing.md`
- OpenAPI candidate resolution: `docs/openapi-candidate-resolution.md`
- Canonical build + persistence: `docs/canonical-spec-build-persistence.md`
- Outbound notifications: `docs/outbound-webhook-notifications.md`
- Database schema baseline: `docs/database-schema-baseline.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
