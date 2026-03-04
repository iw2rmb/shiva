# Canonical Spec Build and Persistence (Item 7)

## Status
- Implemented: canonical spec build from item-6 resolver output, artifact persistence, endpoint index persistence.
- Scope completed:
  - deterministic canonical `spec.json` and `spec.yaml` generation,
  - `spec_artifacts` upsert by `revision_id`,
  - `endpoint_index` extraction and replacement writes per revision.
- Explicitly out of scope for this item:
  - semantic diff implementation details (`spec_changes`, item 8),
  - outbound notifications (item 9),
  - read HTTP routes (item 10).

## Components
- Canonical builder and extractor:
  - file: `internal/openapi/canonical.go`
  - entrypoint: `BuildCanonicalSpec(resolution ResolutionResult)`.
- Worker integration:
  - file: `cmd/shiva/main.go`
  - flow:
    1. item-6 resolver returns changed OpenAPI documents,
    2. canonical build runs only when `OpenAPIChanged=true`,
    3. canonical artifact + endpoint index persist,
    4. revision is marked processed.
- Store persistence:
  - file: `internal/store/spec_artifacts.go`
  - entrypoint: `PersistCanonicalSpec(ctx, PersistCanonicalSpecInput)`.

## Canonical Build Rules
- Canonical root document is selected deterministically:
  - unique normalized candidate paths,
  - lexicographically sorted,
  - first path present in resolver `Documents` is used.
- External local `$ref` (`./x.yaml#/...`) is inlined from resolver-fetched documents.
- Internal `$ref` (`#/...`) remains in place.
- Canonical JSON:
  - marshaled from expanded object graph with stable key ordering semantics.
- Canonical YAML:
  - generated from recursively sorted YAML nodes for deterministic key ordering.
- ETag:
  - strong ETag derived from `sha256(spec_json)`.
- Size:
  - `size_bytes = len(spec_json) + len(spec_yaml)`.

## Endpoint Index Extraction
- Source: canonical document `paths` object.
- Indexed methods: `get`, `put`, `post`, `delete`, `options`, `head`, `patch`, `trace`.
- Stored fields per endpoint:
  - `method`, `path`,
  - optional `operation_id`, optional `summary`,
  - `deprecated` (default false),
  - canonicalized `raw_json` for the operation object.
- Ordering:
  - deterministic sort by `(method, path)` before persistence.

## Persistence Semantics
- Persistence transaction (`PersistCanonicalSpec`):
  1. upsert `spec_artifacts` for `revision_id`,
  2. delete all prior `endpoint_index` rows for `revision_id`,
  3. insert extracted endpoint rows.
- Input normalization/validation:
  - validates non-empty spec/json/yaml/etag,
  - validates endpoint method/path/raw_json,
  - canonicalizes endpoint `raw_json`,
  - rejects duplicate `(method, path)` rows per revision.
- Error handling:
  - canonical build failures are treated as permanent processing failures and mark revision failed.
  - storage failures are retried through existing worker retry logic.

## Tests
- `internal/openapi/canonical_test.go`
  - `TestBuildCanonicalSpec_IdenticalInputProducesStableOutput`
  - `TestBuildCanonicalSpec_ExtractsEndpointIndex`
- `internal/store/spec_artifacts_test.go`
  - `TestPersistCanonicalSpec_UpsertsArtifactAndReplacesEndpointIndex`
  - `TestNormalizePersistCanonicalSpecInput_DuplicateEndpointFails`

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Worker processing: `docs/ingest-worker-processing.md`
- OpenAPI candidate resolution: `docs/openapi-candidate-resolution.md`
- Database baseline: `docs/database-schema-baseline.md`
- Semantic diff engine: `docs/semantic-diff-engine.md`
- Outbound notifications: `docs/outbound-webhook-notifications.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
