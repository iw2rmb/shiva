# Database Schema Baseline (Item 2)

## Status
- Implemented: initial schema migration, sqlc queries, sqlc config, generated package.
- Scope completed: database baseline for tenants/repos/subscriptions/ingest/revisions/artifacts/index/changes/delivery attempts.

## Schema Artifacts
- Migration file: `sql/schema/000001_initial.sql`
- Tables:
  - `tenants`
  - `repos`
  - `subscriptions`
  - `ingest_events`
  - `revisions`
  - `spec_artifacts`
  - `endpoint_index`
  - `spec_changes`
  - `delivery_attempts`

## Query Artifacts
- Query directory: `sql/query/`
- Query files:
  - `tenants.sql`
  - `repos.sql`
  - `subscriptions.sql`
  - `ingest_events.sql`
  - `revisions.sql`
  - `spec_artifacts.sql`
  - `endpoint_index.sql`
  - `spec_changes.sql`
  - `delivery_attempts.sql`

## sqlc Artifacts
- Config: `sqlc.yaml`
- Generated package: `internal/store/sqlc`
- Generation command: `sqlc generate`
- Compilation status: verified by `go test ./...`

## Notes
- Tenant-scoped relation safety is enforced with composite foreign keys where tenant and repo are both present.
- Ingest idempotency keys include both `(repo_id, delivery_id)` and `(repo_id, sha)`.
- `ingest_events` now tracks queue execution state (`attempt_count`, `next_retry_at`) for async retry scheduling.
- Revision and outbound delivery keys include uniqueness constraints for idempotency.
- This baseline is the source of truth for future schema changes. Per policy, changes must be applied by updating CREATE statements in this initial migration.

## References
- Runtime baseline: `docs/runtime-baseline.md`
- GitLab webhook ingest: `docs/gitlab-webhook-ingest.md`
- Worker processing queue: `docs/ingest-worker-processing.md`
- Canonical build + persistence: `docs/canonical-spec-build-persistence.md`
- Semantic diff engine: `docs/semantic-diff-engine.md`
- Outbound notifications: `docs/outbound-webhook-notifications.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
