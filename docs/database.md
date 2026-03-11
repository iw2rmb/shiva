# Database

## Scope
This document describes current schema layout and SQL code generation workflow.

## Schema Source
- Migration source: `sql/schema/000001_initial.sql`
- Query source files: `sql/query/*.sql`
- Generated access layer: `internal/store/sqlc/*`

## Core Tables
- `schema_migrations`: startup schema bootstrap record (`version`, `checksum`, `applied_at`).
- `repos`: repo identity (`gitlab_project_id`, `path_with_namespace`, `default_branch`, `openapi_force_rescan`).
- `startup_index_state`: singleton startup-index checkpoint (`last_project_id`).
- `subscriptions`: outbound webhook subscribers and retry policy.
- `ingest_events`: inbound queue records and canonical repo revision rows (`sha`, `branch`, retry state, terminal processing result).
- `api_specs`: durable API root identity per repo (`root_path`, `status`, optional `display_name`).
- `api_spec_revisions`: per-API per-revision build record (`root_path_at_revision`, `build_status`, `error`) used for both bootstrap and incremental workflows.
- `api_spec_dependencies`: per API-spec revision dependency file set (`api_spec_revision_id`, `file_path`).
- `spec_artifacts`: canonical JSON/YAML artifact per `api_spec_revision_id`.
- `endpoint_index`: `(api_spec_revision_id, method, path)` operation index.
- `spec_changes`: semantic diff payload per API (`api_spec_id`) keyed by `to_api_spec_revision_id`, with optional `from_api_spec_revision_id`.
- `delivery_attempts`: outbound event attempt lifecycle keyed by subscription + API + ingest event + event + attempt (`subscription_id`, `api_spec_id`, `ingest_event_id`, `event_type`, `attempt_no`).

## Processing State Fields
- `ingest_events.status`: `pending | processing | processed | failed`
- `ingest_events.processed_at`: terminal processing timestamp for the canonical repo revision row.
- `ingest_events.openapi_changed`: nullable build result on the canonical repo revision row; set on terminal success and cleared on terminal failure.
- `api_specs.status`: `active | deleted`
- `api_spec_revisions.build_status`: processor writes `processing | processed | failed` during per-root build execution in both bootstrap and incremental loops.
- `delivery_attempts.status`: `pending | retry_scheduled | succeeded | failed`
- `repos.openapi_force_rescan`: `true` when next bootstrap decision should force full repository scan.
- `startup_index_state.last_project_id`: highest GitLab project ID fully handled by startup indexing; startup resumes with GitLab `id_after=<last_project_id>`.

## API Spec Store Primitives
- `ListActiveAPISpecsWithLatestDependencies(repo_id)`: returns active `api_specs` in `root_path` order with dependency file paths from each spec's latest `api_spec_revisions` row where `build_status='processed'` (ties resolved by `ingest_event_id DESC, id DESC`); specs without processed revisions return an empty dependency list.
- `ListAPISpecListingByRepo(repo_id)`: returns all `api_specs` (active + deleted) in `root_path` order with listing fields `api` (`root_path`), `status`, and optional `last_processed_revision` metadata (`api_spec_revision_id`, `ingest_event_id`, `ingest_event_sha`, `ingest_event_branch`).
- `ListAPISpecListingByRepoAtRevision(repo_id, ingest_event_id)`: returns deterministic inventory as of the given canonical ingest event row, using the latest processed API revision with `ingest_event_id <= ingest_event_id`.
- `MarkAPISpecDeleted(api_spec_id)`: sets `api_specs.status='deleted'` for root deactivation flows.
- `api_spec_dependencies` are revision-scoped and only latest-processed rows feed incremental impact intersection.
- `api_spec_revisions.ingest_event_id` and `delivery_attempts.ingest_event_id` both reference the canonical `ingest_events.id`.
- `spec_artifacts` and `endpoint_index` write contracts are strictly `api_spec_revision_id`-scoped.
- `spec_changes` write contracts are `api_spec_id`-scoped and read with `(api_spec_id, to_api_spec_revision_id)`.
- `delivery_attempts` read/write contracts include `api_spec_id` in the dedupe/lookup identity.

### Read Compatibility Behavior
- `GetSpecArtifactByRevisionID` and `GetEndpointIndexByMethodPath` are retained only as compatibility helpers for store-level callers and tests.
- They resolve the latest processed API-scoped row for the requested canonical `ingest_events.id` across all APIs in that ingest event, then return that row only when a matching method/path exists.
- The HTTP read surface no longer uses these helpers; query endpoints resolve through API-scoped snapshot primitives instead.
- Read resolution identifies repos directly by `repos.path_with_namespace`.
- No-selector reads resolve against the repo's persisted `default_branch`, not a global branch constant.

## CLI Snapshot Read Primitives
- `ResolveReadSnapshot(repo, api?, revision_id|sha|default-branch-latest)`: resolves the canonical repo snapshot used by the query-style CLI read contract.
  - `revision_id` and `sha` both anchor to an exact processed repo snapshot, even when `openapi_changed=false`.
  - default-branch reads still resolve through the repo's stored `default_branch` and then pick the latest processed OpenAPI snapshot on that branch.
- Snapshot-scoped API and operation queries for the CLI walk the selected revision's `parent_sha` ancestry chain; they do not assume that `ingest_events.id` ordering alone defines repo history across branches.
- `ListRepoCatalogInventory()`: returns repo catalog rows with repo identity, `active_api_count`, default-branch head state, and latest processed OpenAPI snapshot state.
- `GetRepoCatalogFreshnessByPath(path_with_namespace)`: returns the same freshness metadata for one repo so the CLI can decide whether a cached default-branch catalog slice is stale.
- `ListAPISnapshotInventoryByRepoRevision(repo_id, snapshot_revision_id)`: returns API inventory at a repo snapshot with optional resolved API revision metadata, artifact metadata (`etag`, `size_bytes`), and operation counts.
- `GetAPISnapshotByRepoRevisionAndAPI(repo_id, api, snapshot_revision_id)`: resolves one API row inside a repo snapshot without collapsing “API exists but has no processed snapshot yet” into a lookup error.
- `ListOperationInventoryByRepoRevision(repo_id, snapshot_revision_id)` and `ListOperationInventoryByRepoRevisionAndAPI(...)`: return operation inventory rows with `api`, `method`, `path`, `operation_id`, `summary`, `deprecated`, and `raw_json`.
- `FindOperationCandidatesByRepoRevisionAndOperationID(...)` and API-scoped counterpart: preserve zero/one/many candidate rows so multi-API ambiguity is reported by the caller instead of hidden in the store.
- `FindOperationCandidatesByRepoRevisionAndMethodPath(...)` and API-scoped counterpart: provide the same candidate-preserving lookup for canonical `(method, path)` resolution.

### Query Source Layout
- `sql/query/cli_reads.sql`: snapshot-oriented repo/API/operation inventory and candidate lookup queries used by the CLI/query transport work.
- `sql/schema/000001_initial.sql` includes supporting indexes for:
  - default-branch head and latest processed OpenAPI lookups on `ingest_events`,
  - latest snapshot selection on `api_spec_revisions`,
  - `operation_id` lookup inside `endpoint_index`.

## Generation
sqlc config:
- `sqlc.yaml`
- engine: PostgreSQL
- schema dir: `sql/schema`
- query dir: `sql/query`
- output: `internal/store/sqlc`

Regenerate code:
- `sqlc generate`

## Change Workflow
1. Update `sql/schema/000001_initial.sql`.
2. Update affected `sql/query/*.sql` files.
3. Run `sqlc generate`.
4. Run `go test ./internal/store` and `go test ./...`.

Notes:
- Repository currently keeps schema in the initial migration file.
- Shiva applies the embedded `sql/schema/000001_initial.sql` schema at startup and records version/checksum in `schema_migrations`.
- Startup schema bootstrap is idempotent for fresh databases and repeated restarts, but it is not a multi-step migration system for upgrading arbitrary existing schemas.

## References
- Setup and runtime config: `docs/setup.md`
- GitLab ingestion and canonical revision writes: `docs/gitlab.md`
- Endpoint index read/write behavior: `docs/endpoints.md`
- Webhook delivery attempts: `docs/webhooks.md`
- DB-related test guidance: `docs/testing.md`
