-- name: CreateIngestEvent :one
INSERT INTO ingest_events (
    tenant_id,
    repo_id,
    sha,
    branch,
    parent_sha,
    event_type,
    delivery_id,
    payload_json
)
VALUES (
    sqlc.arg(tenant_id),
    sqlc.arg(repo_id),
    sqlc.arg(sha),
    sqlc.arg(branch),
    sqlc.narg(parent_sha),
    sqlc.arg(event_type),
    sqlc.arg(delivery_id),
    sqlc.arg(payload_json)
)
RETURNING id, tenant_id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, status, error;

-- name: GetIngestEventByRepoDelivery :one
SELECT id, tenant_id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND delivery_id = sqlc.arg(delivery_id);

-- name: GetIngestEventByRepoSHA :one
SELECT id, tenant_id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND sha = sqlc.arg(sha);

-- name: ClaimNextIngestEvent :one
WITH candidate AS (
    SELECT id
    FROM ingest_events AS ie
    WHERE ie.status = 'pending'
      AND ie.next_retry_at <= NOW()
      AND NOT EXISTS (
          SELECT 1
          FROM ingest_events AS older
          WHERE older.repo_id = ie.repo_id
            AND older.id < ie.id
            AND older.status IN ('pending', 'processing')
      )
    ORDER BY ie.id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE ingest_events
SET status = 'processing',
    attempt_count = ingest_events.attempt_count + 1,
    error = ''
FROM candidate
WHERE ingest_events.id = candidate.id
RETURNING ingest_events.id, ingest_events.tenant_id, ingest_events.repo_id, ingest_events.sha, ingest_events.branch, ingest_events.parent_sha, ingest_events.event_type, ingest_events.delivery_id, ingest_events.payload_json, ingest_events.received_at, ingest_events.attempt_count, ingest_events.next_retry_at, ingest_events.status, ingest_events.error;

-- name: MarkIngestEventProcessed :one
UPDATE ingest_events
SET status = 'processed',
    error = ''
WHERE id = sqlc.arg(id)
RETURNING id, tenant_id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, status, error;

-- name: ScheduleIngestEventRetry :one
UPDATE ingest_events
SET status = 'pending',
    error = sqlc.arg(error),
    next_retry_at = sqlc.arg(next_retry_at)
WHERE id = sqlc.arg(id)
RETURNING id, tenant_id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, status, error;

-- name: MarkIngestEventFailed :one
UPDATE ingest_events
SET status = 'failed',
    error = sqlc.arg(error)
WHERE id = sqlc.arg(id)
RETURNING id, tenant_id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, status, error;
