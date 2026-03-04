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
   - bounded parallel candidate file fetches,
   - extension and top-level sniff filtering on a configurable byte prefix,
   - root validation and local `$ref` closure resolution.
   - per discovered root:
     - upsert `api_specs` by `root_path`,
     - write `api_spec_revisions` status rows,
     - replace `api_spec_dependencies`,
     - build/persist canonical artifact + endpoint index (revision-level storage).
6. Incremental mode runs:
   - GitLab Compare API (`from=parent_sha`, `to=sha`) once to load changed paths.
   - Load active `api_specs` with latest dependency sets.
   - Resolve impacted APIs by changed-path intersection against `{root_path + dependency_paths}`.
   - For impacted APIs:
     - if root path is deleted in compare, mark API spec status as `deleted`;
     - otherwise, resolve that root at `sha`, rebuild it, and persist updated dependency set.
   - If no impacted APIs were found and compare includes `new_file` or `renamed_file` paths, run targeted discovery on those changed paths and create/build newly valid roots.
7. If at least one API was rebuilt:
   - build canonical JSON+YAML,
   - extract endpoints,
   - persist `spec_artifacts` and `endpoint_index`,
   - compute and persist semantic diff.
   - Note: deleted-root deactivation without any rebuilt API updates `api_specs` status only; revision-level canonical artifact/diff is not rebuilt in that case.
8. Mark revision processed and emit outbound notifications.
9. On successful bootstrap completion, clear `repos.openapi_force_rescan`.

## GitLab APIs Used
- `GET /projects/:id/repository/compare?from=<fromSHA>&to=<toSHA>`
- `GET /projects/:id/repository/files/:path?ref=<sha>`
- `GET /projects/:id/repository/tree?ref=<sha>&recursive=true`

No clone/archive strategy is used.

## Candidate Detection Rules
Default include globs:
- `**/openapi*.{yaml,yml,json}`
- `**/swagger*.{yaml,yml,json}`
- `**/api/**/*.yaml`

Resolver config is controlled by:
- `SHIVA_OPENAPI_PATH_GLOBS`
- `SHIVA_OPENAPI_REF_MAX_FETCHES`
- `SHIVA_OPENAPI_BOOTSTRAP_FETCH_CONCURRENCY`
- `SHIVA_OPENAPI_BOOTSTRAP_SNIFF_BYTES`

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
Bootstrap now persists per-root API metadata (`api_specs`, `api_spec_revisions`, `api_spec_dependencies`) and isolates root-local permanent build failures so other roots can still build.

Canonical artifact/index/change tables are still keyed by `revision_id`. In multi-root bootstrap, later successful root builds overwrite earlier root artifact/index rows for the same revision.

## References
- Setup and envs: `docs/setup.md`
- Endpoint extraction and read routes: `docs/endpoints.md`
- Inbound/outbound webhook contracts: `docs/webhooks.md`
- Database state model: `docs/database.md`
- Tests: `docs/testing.md`
