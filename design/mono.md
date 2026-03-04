# Monorepo + Bootstrap Discovery Design

## Scope
This design covers three related subjects:
- initial build bootstrap when no known OpenAPI roots exist for a repo,
- incremental updates after roots are known,
- monorepo support with multiple OpenAPI specs per repository.

The design assumes no backward compatibility constraints.

## Problem
Current resolver flow is delta-only (`compare(parent_sha, sha)` + changed-path filtering). This misses existing specs when first processed revision does not touch OpenAPI files. The current storage/read model also assumes one canonical artifact per revision, which is not correct for monorepos with multiple specs.

## Goals
- Guarantee first successful discovery of OpenAPI roots even when first webhook is unrelated.
- Keep incremental processing bounded to impacted specs only.
- Represent and serve multiple specs per repo as first-class entities.
- Support ignore rules (`.shivaignore`) to avoid scanning test/generated/noise paths.

## Non-Goals
- Supporting remote `$ref` targets.
- Supporting multiple Git providers in this iteration.
- Preserving current single-spec schema contracts.

## Terminology
- API root: OpenAPI entry file (`openapi`/`swagger` at top level) that defines one spec.
- API graph: closure of local `$ref` files reachable from one root.
- API instance: stable identity of one root within one repo.
- Impacted API: API instance whose root/graph intersects changed files in a revision.

## High-Level Model
1. Discover API roots per repo at a specific `sha`.
2. Persist API instances (`root_path` + metadata) as durable identities.
3. For each revision, resolve impacted API instances.
4. Build canonical artifact/index/diff per impacted API instance.
5. Read API resolves selector + API instance, then serves full spec or endpoint slices.

## Initial Build Bootstrap

### Trigger
Bootstrap discovery runs when either condition is true:
- repo has zero active API instances,
- forced rescan flag is set for the repo.

### Source of Files
Add GitLab tree listing in client surface:
- `ListRepositoryTree(ctx, projectID, sha, path, recursive)` with pagination.

Discovery works from full tree at `sha` (not from compare diffs).

### Candidate Filtering Pipeline
1. Path normalization.
2. Ignore filtering:
   - built-in defaults (for example `**/test*/**`, `**/.git/**`),
   - `.shivaignore` patterns from repo root at same `sha` if file exists.
3. Extension prefilter (`.yaml`, `.yml`, `.json`).
4. Fast content sniff:
   - YAML: line-level match for top-level `openapi:` or `swagger:`,
   - JSON: object-level match for top-level `"openapi"` or `"swagger"`.
5. Strict parse/validate using current resolver parser.

Only files passing step 5 become API roots.

### Bootstrap Result
For each discovered root:
- resolve local `$ref` closure,
- build canonical artifact,
- upsert API instance and per-revision artifact/index,
- mark revision processed with `openapi_changed=true` if at least one API built.

If zero roots found:
- mark revision processed with `openapi_changed=false`.

## `.shivaignore` Specification

### Location
- Optional file at repository root: `.shivaignore`.

### Syntax
- One pattern per line.
- `#` starts comment.
- Empty lines ignored.
- Patterns use doublestar semantics.
- Leading `/` anchors to repo root.
- Negation (`!`) is not supported in first iteration.

### Effective Ignore Set
`effective_ignores = built_in_ignores + file_ignores`

Built-in defaults include:
- `**/test*/**`
- `**/__tests__/**`
- `**/node_modules/**`
- `**/vendor/**`

## Incremental Updates

### Input
- Changed paths from GitLab compare (`parent_sha -> sha`).

### Impact Resolution
For each active API instance in repo:
- load latest known dependency set for that API instance,
- mark impacted if any changed path intersects:
  - root path,
  - any dependency path in API graph.

If no API instances exist:
- run bootstrap discovery instead.

### Build Rules
- Rebuild only impacted API instances.
- Non-impacted API instances are carried forward logically (no rebuild for this revision).
- Deleted root file deactivates API instance and emits removal diff/event for that API.

### Fallback Rule
If changed paths contain create/rename events matching discovery heuristics and no impacted APIs were found, run targeted discovery on changed files to detect newly added roots.

## Monorepo Data Model

### New Tables
- `api_specs`
  - `id`, `repo_id`, `root_path`, `display_name`, `status` (`active|deleted`), timestamps.
  - unique: `(repo_id, root_path)`.

- `api_spec_revisions`
  - `id`, `api_spec_id`, `revision_id`, `root_path_at_revision`, `build_status`, `error`, timestamps.
  - unique: `(api_spec_id, revision_id)`.

- `api_spec_dependencies`
  - `api_spec_revision_id`, `file_path`.
  - unique: `(api_spec_revision_id, file_path)`.

### Existing Tables Refactor
- `spec_artifacts`: replace `revision_id` key with `api_spec_revision_id`.
- `endpoint_index`: key by `api_spec_revision_id`.
- `spec_changes`: key by `api_spec_id` + from/to `api_spec_revision_id`.
- notification delivery identity includes `api_spec_id`.

## Processing Pipeline Changes
- Revision processor first resolves impacted API instances.
- Build/diff/notify loops over impacted APIs.
- Revision-level `openapi_changed` is true if any API instance changed.
- Failures are isolated per API instance; one API failure does not corrupt identities of others.

## Read API Contract (Monorepo-Aware)

### API Identity in Routes
Read routes include API selector segment:
- `GET /{tenant}/{repo}/{api}.{json|yaml}`
- `GET /{tenant}/{repo}/{selector}/{api}.{json|yaml}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /{tenant}/{repo}/{api}/{path}`
- `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /{tenant}/{repo}/{selector}/{api}/{path}`

`api` is URL-encoded `root_path` (stable and deterministic).

### Discovery Endpoint
Add listing route for UX/discovery:
- `GET /{tenant}/{repo}/apis`
- `GET /{tenant}/{repo}/{selector}/apis`

Response includes `api` (root path), status, last processed revision.

## Notifications
Webhook payload includes:
- `api`: root path identity,
- `api_revision_id`,
- URLs scoped to that API instance.

Subscribers can filter by API instance.

## Failure Handling
- Invalid root or invalid `$ref` in one API instance marks that API revision failed.
- Other impacted APIs in same repo revision continue.
- Fatal repo-level errors (GitLab auth/network outage, DB outage) still fail whole job.

## Performance Constraints
- Bootstrap discovery uses paginated tree listing and bounded concurrency for content fetch.
- Discovery sniff reads only small prefix first when possible.
- Incremental mode avoids full-tree scans by dependency intersection.

## Rollout Plan
1. Add schema/entities for `api_specs` and per-API revisions/dependencies.
2. Add bootstrap discovery mode + `.shivaignore` parser.
3. Refactor build persistence to per-API artifacts/index.
4. Refactor diff/notify to per-API semantics.
5. Introduce monorepo-aware read routes and `/apis` listing.
6. Remove legacy single-spec assumptions.

## Tests
- Bootstrap:
  - repo with unrelated first webhook still discovers existing root(s),
  - `.shivaignore` excludes test fixtures,
  - malformed `.shivaignore` line handling.
- Incremental:
  - dependency intersection triggers rebuild,
  - unrelated file change does not rebuild,
  - new root added in changed files is discovered.
- Monorepo:
  - two roots in one repo produce independent artifacts/diffs,
  - one API failure does not block other API build,
  - read routes resolve correct API by root path.

## Open Questions
- Should `.shivaignore` support negation (`!pattern`) in v1?
- Should API identity be only `root_path` or `(root_path + optional alias)`?
- Do we need per-API subscription records, or is payload-level filtering enough?

## References
- Base architecture: `design/shiva.md`
- OpenAPI resolver behavior: `docs/openapi-candidate-resolution.md`
- Canonical persistence: `docs/canonical-spec-build-persistence.md`
- Worker processing: `docs/ingest-worker-processing.md`
