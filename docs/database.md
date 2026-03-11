# Database

## Scope
This document describes current schema layout and SQL code generation workflow.

## Schema Source
- Migration source: `sql/schema/000001_initial.sql`
- Query source files: `sql/query/*.sql`
- Generated access layer: `internal/store/sqlc/*`

## Core Tables
- `schema_migrations`: startup schema bootstrap record (`version`, `checksum`, `applied_at`).
- `tenants`: tenant identity.
- `repos`: repo identity per tenant (`gitlab_project_id`, `path_with_namespace`, `default_branch`, `openapi_force_rescan`).
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
- `GetSpecArtifactByRevisionID` and `GetEndpointIndexByMethodPath` are retained for legacy read routes.
- They resolve the latest processed API-scoped row for the requested canonical `ingest_events.id` across all APIs in that ingest event, then return that row only when a matching method/path exists.
- Monorepo read routes (`/-/{api}/-/`) still resolve explicitly via API root and revision ID, so same revision + different API returns different results.

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
