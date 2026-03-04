# Database

## Scope
This document describes current schema layout and SQL code generation workflow.

## Schema Source
- Migration source: `sql/schema/000001_initial.sql`
- Query source files: `sql/query/*.sql`
- Generated access layer: `internal/store/sqlc/*`

## Core Tables
- `tenants`: tenant identity.
- `repos`: repo identity per tenant (`gitlab_project_id`, `path_with_namespace`, `default_branch`).
- `subscriptions`: outbound webhook subscribers and retry policy.
- `ingest_events`: inbound queue records and retry state.
- `revisions`: per-repo revision processing state.
- `spec_artifacts`: canonical JSON/YAML artifact per revision.
- `endpoint_index`: `(revision_id, method, path)` operation index.
- `spec_changes`: semantic diff payload per `to_revision_id`.
- `delivery_attempts`: outbound event attempt lifecycle.

## Processing State Fields
- `ingest_events.status`: `pending | processing | processed | failed`
- `revisions.status`: `pending | processed | failed | skipped`
- `delivery_attempts.status`: `pending | retry_scheduled | succeeded | failed`

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
- Migration runner/tooling is not bundled in this repository.

## References
- Setup and runtime config: `docs/setup.md`
- GitLab ingestion and revision writes: `docs/gitlab.md`
- Endpoint index read/write behavior: `docs/endpoints.md`
- Webhook delivery attempts: `docs/webhooks.md`
- DB-related test guidance: `docs/testing.md`
