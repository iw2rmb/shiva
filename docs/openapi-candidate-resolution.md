# OpenAPI Candidate Detection and Resolution (Item 6)

## Status
- Implemented: OpenAPI candidate detection and recursive local `$ref` resolution.
- Scope completed:
  - configurable include globs,
  - top-level `openapi`/`swagger` validation,
  - recursive local `$ref` fetch via GitLab Repository Files API,
  - cycle detection and fetch limits.
- Explicitly out of scope for this item:
  - canonical artifact persistence/index (item 7),
  - diff/notify/read API work (items 8-10).

## Components
- Resolver:
  - file: `internal/openapi/resolver.go`
  - method: `ResolveChangedOpenAPI(ctx, client, projectID, fromSHA, toSHA)`
  - uses item-5 GitLab client surface:
    - `CompareChangedPaths`
    - `GetFileContent`
- Worker integration:
  - file: `cmd/shiva/main.go`
  - `revisionProcessor` now runs candidate detection/resolution after revision upsert.
  - revision status transitions:
    - success: `MarkRevisionProcessed(openapi_changed=true|false)`
    - permanent OpenAPI failures: `MarkRevisionFailed(...)` + permanent worker error

## Configuration
- `SHIVA_GITLAB_BASE_URL`: GitLab host URL used by the worker client.
- `SHIVA_GITLAB_TOKEN`: optional private token for GitLab API calls.
- `SHIVA_OPENAPI_PATH_GLOBS`: comma-separated include globs.
  - default:
    - `**/openapi*.{yaml,yml,json}`
    - `**/swagger*.{yaml,yml,json}`
    - `**/api/**/*.yaml`
- `SHIVA_OPENAPI_REF_MAX_FETCHES`: max fetched file count per resolution run.
  - default: `128`

## Detection and Resolution Flow
1. Load changed paths via GitLab compare API (`from=<parent_sha>`, `to=<sha>`).
2. Filter changed paths by configured include globs.
3. For each matched non-deleted path:
  - fetch file content via Repository Files API,
  - parse YAML/JSON,
  - require top-level `openapi` or `swagger`.
4. For each validated root document:
  - recursively collect local `$ref` targets,
  - resolve relative targets by source file directory,
  - fetch each target through Repository Files API at the same `ref=<sha>`.
5. Enforce safeguards:
  - cycle detection in active `$ref` DFS stack,
  - max fetched file count limit.

## Error Semantics
- Invalid candidate/root document: `ErrInvalidOpenAPIDocument`.
- Invalid or non-local reference: `ErrInvalidReference`.
- Cyclic references: `ErrReferenceCycle`.
- Fetch limit breach: `ErrFetchLimitExceeded`.
- Missing referenced file (`gitlab.ErrNotFound`) is treated as permanent processing failure.

## Tests
- `internal/openapi/resolver_test.go`
  - `TestResolverResolveChangedOpenAPI_MultiFileSuccess`
  - `TestResolverResolveChangedOpenAPI_InvalidTopLevelDocument`
  - `TestResolverResolveChangedOpenAPI_ReferenceCycle`
  - `TestResolverResolveChangedOpenAPI_ReferenceFetchLimit`

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Worker processing: `docs/ingest-worker-processing.md`
- GitLab client integrations: `docs/gitlab-client-integrations.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
