# Ingest Worker Processing (Item 4)

## Status
- Implemented: ordered async processing by repository with retry/backoff.
- Scope completed:
  - queue abstraction and worker pool under `internal/worker`
  - repo-keyed ordering by `repo_id`
  - idempotency key `(repo_id, sha)` in ingest persistence.

## Components
- Worker manager:
  - file: `internal/worker/manager.go`
  - runs configurable worker pool (`SHIVA_WORKER_CONCURRENCY`)
  - claims queue jobs, runs processor, marks processed/retry/failed.
- Queue backoff:
  - file: `internal/worker/backoff.go`
  - exponential backoff with bounded max duration.
- DB-backed queue methods:
  - file: `internal/store/worker_queue.go`
  - uses sqlc queries from `sql/query/ingest_events.sql`.

## Queue Semantics
- Event claim is atomic (`ClaimNextIngestEvent`) and updates status to `processing`.
- Claim eligibility:
  - `status='pending'`
  - `next_retry_at <= NOW()`
  - no older event for the same repo in `pending` or `processing`.
- This enforces per-repo commit-order processing while allowing cross-repo parallelism.

## Retry Semantics
- Failed processing attempts are rescheduled via `ScheduleIngestEventRetry`.
- Next retry time is computed by exponential backoff.
- After `max_attempts` (default `5`) or a permanent error, event is marked `failed`.

## Idempotency
- Ingest-level idempotency:
  - `(repo_id, delivery_id)` for duplicate webhook deliveries
  - `(repo_id, sha)` for duplicate commit processing requests.
- Processor-level idempotency:
  - `revisions` upsert keeps `(repo_id, sha)` unique.

## Tests
- `internal/worker/manager_test.go`
  - `TestManager_ProcessesSameRepoInCommitOrder`
  - `TestManager_RetriesWithBackoff`

## References
- Runtime wiring: `docs/runtime-baseline.md`
- Webhook ingest: `docs/gitlab-webhook-ingest.md`
- Database schema: `docs/database-schema-baseline.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
