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
     - build/persist canonical artifact + endpoint index keyed by `api_spec_revision_id`.
6. Incremental mode runs:
   - GitLab Compare API (`from=parent_sha`, `to=sha`) once to load changed paths.
   - Normalize changed paths and load active `api_specs` with latest `processed` dependency snapshots.
   - Build impacted set from changed-path intersection against `{root_path + dependency_paths}`.
   - Rename changes contribute both `old_path` and `new_path` to impact checks.
   - For each impacted API:
     - if the root path is deleted in compare, call `MarkAPISpecDeleted`.
     - otherwise, write `api_spec_revisions` `processing`, resolve/build that root at `sha`, then write terminal status:
       - `processed` on success,
       - `failed` on permanent root-local errors (invalid root/`$ref`/cycle/fetch-limit/not-found/GitLab 4xx), while continuing with other APIs.
   - If no impacted APIs are found, run targeted discovery on `new_file` / `renamed_file` candidate paths and create/build newly valid roots.
7. For each rebuilt API:
   - build canonical JSON+YAML,
   - extract endpoints,
   - persist `spec_artifacts` and `endpoint_index` for that API spec revision.
8. For each changed API (rebuilt or deactivated root):
   - compute and persist semantic diff (`spec_changes`) for that API.
   - mark revision `openapi_changed=true`.
   - create API-scoped revision rows and downstream identities per API (`api_spec_id`).
9. If no API was rebuilt and no API was deactivated:
   - mark revision `openapi_changed=false`.
10. Emit outbound notifications:
   - emit events per changed API (`spec.updated.diff`, optional `spec.updated.full`),
   - always emit `spec.updated.diff` for each changed API,
   - emit `spec.updated.full` only when canonical artifact exists for that API spec revision.
11. On successful bootstrap completion, clear `repos.openapi_force_rescan`.

## Incremental vs Bootstrap Matrix
| Dimension | Bootstrap | Incremental |
|---|---|---|
| Trigger | `active_api_count == 0` or `openapi_force_rescan == true` | `active_api_count > 0` and `openapi_force_rescan == false` |
| Discovery scope | Full tree at `sha` | Changed paths from one compare (`from=parent_sha`,`to=sha`) |
| Rebuild scope | Every discovered root | Impacted roots only, plus fallback discovery when no impacts and create/rename candidates exist |
| Dependency use | Replaces `api_spec_dependencies` for each discovered root | Reuses latest `api_spec_dependencies` from processed revisions to detect impact |
| Root deletion | No deletion pass | Deletes matching impacted roots by setting `api_specs.status='deleted'` |
| Artifact/diff outputs | `openapi_changed=true` if any root built | `openapi_changed=true` if any API build succeeds or impacted root is deleted; diff is emitted per changed API revision, full is emitted per API revision only when artifact exists |

## GitLab APIs Used
- `GET /projects/:id/repository/compare?from=<fromSHA>&to=<toSHA>`
- `GET /projects/:id/repository/files/:path?ref=<sha>`
- `GET /projects/:id/repository/tree?ref=<sha>&recursive=true`

No clone/archive strategy is used.

## GitLab Client Request Policy
- Shiva serializes GitLab API calls per configured GitLab host with an in-process limiter.
- Each request uses retry/backoff locally before the worker-level retry loop sees the error.
- Transport failures and GitLab `5xx` responses retry with exponential backoff starting at `500ms` and capped at `30s`.
- GitLab `429 Too Many Requests` responses honor `Retry-After` when GitLab returns either delta-seconds or an HTTP date.
- Other GitLab `4xx` responses get one confirmation retry, then surface as permanent GitLab errors.
- Terminal `404` responses still map to the resolver's not-found path handling.

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

Incremental mode exception for impacted/fallback per-API execution:
- the same permanent error classes are isolated to that API (`api_spec_revisions` `failed`) and do not fail the whole revision when other APIs can proceed.

Other errors are retried by worker backoff policy.

Cross-failure behavior:
- Compare/diff/persist failures in incremental mode are revision-scoped and can fail the whole revision.
- Per-API/per-root permanent processing errors in incremental mode are scoped to that root and do not block other impacted roots.

## References
- Setup and envs: `docs/setup.md`
- Endpoint extraction and read routes: `docs/endpoints.md`
- Inbound/outbound webhook contracts: `docs/webhooks.md`
- Database state model: `docs/database.md`
- Tests: `docs/testing.md`
