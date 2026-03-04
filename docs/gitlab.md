# GitLab Ingestion

## Scope
This document describes how Shiva ingests specs from GitLab and turns revisions into canonical OpenAPI artifacts.

## Inbound to Build Pipeline
1. `POST /internal/webhooks/gitlab` persists ingest event and repo metadata.
2. Worker claims pending ingest events from DB queue.
3. Worker upserts revision from ingest event.
4. If `parent_sha` is empty, revision is marked processed with `openapi_changed=false`.
5. Otherwise resolver runs:
   - GitLab Compare API (`from=parent_sha`, `to=sha`) to get changed paths,
   - candidate filtering by include globs,
   - candidate file fetch via GitLab Repository Files API at `ref=sha`,
   - parse and validate top-level `openapi` or `swagger`,
   - recursive local `$ref` fetch/resolve with cycle and max-fetch guards.
6. If resolver says OpenAPI changed:
   - build canonical JSON+YAML,
   - extract endpoints,
   - persist `spec_artifacts` and `endpoint_index`,
   - compute and persist semantic diff.
7. Mark revision processed and emit outbound notifications.

## GitLab APIs Used
- `GET /projects/:id/repository/compare?from=<fromSHA>&to=<toSHA>`
- `GET /projects/:id/repository/files/:path?ref=<sha>`

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
Current ingestion is delta-based from `compare(parent_sha, sha)`. Shiva does not perform initial full-tree bootstrap discovery yet.

## References
- Setup and envs: `docs/setup.md`
- Endpoint extraction and read routes: `docs/endpoints.md`
- Inbound/outbound webhook contracts: `docs/webhooks.md`
- Database state model: `docs/database.md`
- Tests: `docs/testing.md`
