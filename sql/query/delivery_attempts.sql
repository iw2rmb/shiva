-- name: CreateDeliveryAttempt :one
INSERT INTO delivery_attempts (
    subscription_id,
    api_spec_id,
    ingest_event_id,
    event_type,
    attempt_no,
    status,
    next_retry_at
)
VALUES (
    sqlc.arg(subscription_id),
    sqlc.arg(api_spec_id),
    sqlc.arg(ingest_event_id),
    sqlc.arg(event_type),
    sqlc.arg(attempt_no),
    sqlc.arg(status),
    sqlc.narg(next_retry_at)
)
RETURNING id, subscription_id, api_spec_id, ingest_event_id, event_type, attempt_no, status, response_code, error, next_retry_at, created_at, updated_at;

-- name: UpdateDeliveryAttemptResult :one
UPDATE delivery_attempts
SET status = sqlc.arg(status),
    response_code = sqlc.narg(response_code),
    error = sqlc.arg(error),
    next_retry_at = sqlc.narg(next_retry_at),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING id, subscription_id, api_spec_id, ingest_event_id, event_type, attempt_no, status, response_code, error, next_retry_at, created_at, updated_at;

-- name: ListDueDeliveryAttempts :many
SELECT id, subscription_id, api_spec_id, ingest_event_id, event_type, attempt_no, status, response_code, error, next_retry_at, created_at, updated_at
FROM delivery_attempts
WHERE status IN ('pending', 'retry_scheduled')
  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
ORDER BY created_at
LIMIT sqlc.arg(limit_count);

-- name: GetLatestDeliveryAttemptByKey :one
SELECT id, subscription_id, api_spec_id, ingest_event_id, event_type, attempt_no, status, response_code, error, next_retry_at, created_at, updated_at
FROM delivery_attempts
WHERE subscription_id = sqlc.arg(subscription_id)
  AND api_spec_id = sqlc.arg(api_spec_id)
  AND ingest_event_id = sqlc.arg(ingest_event_id)
  AND event_type = sqlc.arg(event_type)
ORDER BY attempt_no DESC
LIMIT 1;
