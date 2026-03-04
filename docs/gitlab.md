# GitLab Ingestion

## Scope
This document describes how Shiva ingests specs from GitLab and turns revisions into canonical OpenAPI artifacts.

## Inbound to Build Pipeline
1. `POST /internal/webhooks/gitlab` persists ingest event and repo metadata.
2. Worker claims pending ingest events from DB queue.
3. Worker upserts revision from ingest event.
4. Worker loads bootstrap state (`active_api_count`, `openapi_force_rescan`) and decides ingestion mode:
   - bootstrap when `active_api_count == 0` or `openapi_force_rescan == true`,
   - incremental otherwise.
5. Bootstrap mode resolver runs:
   - GitLab Repository Tree API at `ref=sha` for full repo file enumeration,
   - `.shivaignore` + built-in ignore filtering,
   - extension and top-level sniff filtering,
   - root validation and local `$ref` closure resolution.
6. Incremental mode resolver runs:
   - GitLab Compare API (`from=parent_sha`, `to=sha`) to get changed paths,
   - candidate filtering by include globs,
   - candidate file fetch via GitLab Repository Files API at `ref=sha`,
   - parse and validate top-level `openapi` or `swagger`,
   - recursive local `$ref` fetch/resolve with cycle and max-fetch guards.
7. If resolver says OpenAPI changed:
   - build canonical JSON+YAML,
   - extract endpoints,
   - persist `spec_artifacts` and `endpoint_index`,
   - compute and persist semantic diff.
8. Mark revision processed and emit outbound notifications.

## GitLab APIs Used
- `GET /projects/:id/repository/compare?from=<fromSHA>&to=<toSHA>`
- `GET /projects/:id/repository/files/:path?ref=<sha>`
- `GET /projects/:id/repository/tree?ref=<sha>&recursive=true` (resolver bootstrap entrypoint, not yet wired into worker orchestration)

No clone/archive strategy is used.

## Candidate Detection Rules
Default include globs:
- `**/openapi*.{yaml,yml,json}`
- `**/swagger*.{yaml,yml,json}`
- `**/api/**/*.yaml`

Resolver config is controlled by:
- `SHIVA_OPENAPI_PATH_GLOBS`
- `SHIVA_OPENAPI_REF_MAX_FETCHES`

## Permanent vs Retryable Failures
Marked permanent (revision failed, no further retries for that event):
- invalid OpenAPI document,
- invalid local `$ref`,
- `$ref` cycle,
- fetch limit exceeded,
- canonical root/reference errors,
- GitLab `404` and other GitLab 4xx API errors.

Other errors are retried by worker backoff policy.

## Current Limitation
Bootstrap routing is now active, but build persistence is still revision-level single-spec storage.
Per-discovered-root persistence (`api_spec_revisions`, dependency rows, and per-root build loop semantics) is not wired yet.

## References
- Setup and envs: `docs/setup.md`
- Endpoint extraction and read routes: `docs/endpoints.md`
- Inbound/outbound webhook contracts: `docs/webhooks.md`
- Database state model: `docs/database.md`
- Tests: `docs/testing.md`
