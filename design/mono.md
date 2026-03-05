# Monorepo OpenAPI Support Design

## Scope
This design defines monorepo support where one repository can contain multiple independent OpenAPI roots. Initial full-repo ingestion is defined in `design/init.md`. Incremental updates are defined in `design/inc.md`.

The design assumes no backward compatibility constraints.

## Problem
Current storage, indexing, and read contracts treat one revision as one canonical spec. That model breaks for monorepos where several OpenAPI roots must coexist, evolve independently, and be addressable directly.

## Goals
- Represent each OpenAPI root as a first-class durable identity.
- Persist artifacts, indexes, and changes per API instance, not per repo revision.
- Isolate processing failures so one API instance does not block others in the same repo.
- Expose read and notification contracts scoped to a specific API instance.

## Non-Goals
- Supporting remote `$ref` targets.
- Supporting multiple Git providers in this iteration.
- Preserving legacy single-spec schema contracts.

## Terminology
- API root: OpenAPI entry file (`openapi`/`swagger` at top level) that defines one spec.
- API graph: closure of local `$ref` files reachable from one root.
- API instance: stable identity of one root within one repo.
- Impacted API: API instance whose root/graph intersects changed files in a revision.

## Data Model

### New Tables
- `api_specs`
  - `id`, `repo_id`, `root_path`, `display_name`, `status` (`active|deleted`), timestamps.
  - `display_name` source: `info.title` from the latest successful root build; fallback to `root_path` when absent.
  - unique: `(repo_id, root_path)`.
- `api_spec_revisions`
  - `id`, `api_spec_id`, `revision_id`, `root_path_at_revision`, `build_status`, `error`, timestamps.
  - unique: `(api_spec_id, revision_id)`.
- `api_spec_dependencies`
  - `api_spec_revision_id`, `file_path`.
  - unique: `(api_spec_revision_id, file_path)`.

### Existing Tables Refactor
- This is a breaking replacement of current revision-keyed artifact/index/change storage.
- `spec_artifacts`: replace `revision_id` key with `api_spec_revision_id`.
- `endpoint_index`: key by `api_spec_revision_id`.
- `spec_changes`: key by `api_spec_id` plus from/to `api_spec_revision_id`.
- Notification delivery identity includes `api_spec_id`.

## Processing Model
- Revision processing resolves impacted API instances first (via `design/inc.md` rules).
- Build, diff, and notification loops run per impacted API instance.
- Revision-level `openapi_changed` is `true` if any API instance changed.
- Failure is isolated per API instance; repo-level infra failures still fail the whole job.

## Read API Contract

### Route Namespace
- Shiva routes use `/v1/...`.
- Service routes (webhooks, internal endpoints) stay under `/v1/...` outside `specs` and `routes`.
- Spec/listing routes are under `/v1/specs/...`.
- Endpoint routes are under `/v1/routes/...`.

### Selector Contract
- `selector` is optional.
- `selector` format is short SHA: exactly 8 lowercase hex chars (`sha[:8]`).
- Missing selector resolves to `HEAD` on `main`.
- Branch remains stored in DB metadata; branch is not part of read-route selector.

### API Identity in Routes
- Single-spec routes do not include API segment.
- Monorepo routes require API segment with delimiter guards:
  - `/-/{api}/-/`
- `api` is raw `root_path` (not URL-encoded), bounded by `/-/` on both sides.

Specs routes:
- Single-spec:
  - `GET /v1/specs/{tenant}/{repo}/{openapi|index}.{yaml|json}`
  - `GET /v1/specs/{tenant}/{repo}/{selector}/{openapi|index}.{yaml|json}`
- Monorepo:
  - `GET /v1/specs/{tenant}/{repo}/-/{api}/-/{openapi|index}.{yaml|json}`
  - `GET /v1/specs/{tenant}/{repo}/-/{api}/-/{selector}/{openapi|index}.{yaml|json}`

Endpoint routes:
- Single-spec:
  - `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{tenant}/{repo}/{path}`
  - `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{tenant}/{repo}/{selector}/{path}`
- Monorepo:
  - `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{tenant}/{repo}/-/{api}/-/{path}`
  - `{GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE} /v1/routes/{tenant}/{repo}/-/{api}/-/{selector}/{path}`

### API Listing Routes
- `GET /v1/specs/{tenant}/{repo}/apis`
- `GET /v1/specs/{tenant}/{repo}/{selector}/apis`

Response includes `api` (root path), status, and last processed revision.

## Notifications
Webhook payload includes:
- `api`: root path identity.
- `api_revision_id`.
- URLs scoped to that API instance.

Subscribers can filter by API instance.

## Rollout Plan
1. Add `api_specs`, `api_spec_revisions`, and `api_spec_dependencies` schema/entities.
2. Refactor artifact/index/change storage keys to `api_spec_revision_id` and `api_spec_id`.
3. Refactor build/diff/notify execution to per-API loops with failure isolation.
4. Introduce monorepo-aware read routes and `/apis` listing routes.
5. Remove legacy single-spec assumptions.

## Tests
- Two roots in one repo produce independent artifacts and diffs.
- One API failure does not block another API build in same repo revision.
- Read routes resolve the correct API by root path and respect `/-/{api}/-/` delimiter parsing.
- Selector routes accept only 8-char SHA selector and default to `HEAD` on `main` when omitted.
- Notification payloads include stable API identity.

## Open Questions
- Should API identity remain only `root_path` or become `(root_path + optional alias)`?
- Do we need per-API subscription records, or is payload-level filtering enough?

## References
- Initial full-repo ingestion: `design/init.md`
- Incremental impact processing: `design/inc.md`
- GitLab ingestion behavior: `docs/gitlab.md`
- Endpoint extraction/read routing: `docs/endpoints.md`
