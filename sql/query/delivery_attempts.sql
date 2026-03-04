-- name: CreateDeliveryAttempt :one
INSERT INTO delivery_attempts (
    subscription_id,
    revision_id,
    event_type,
    attempt_no,
    status,
    next_retry_at
)
VALUES (
    sqlc.arg(subscription_id),
    sqlc.arg(revision_id),
    sqlc.arg(event_type),
    sqlc.arg(attempt_no),
    sqlc.arg(status),
    sqlc.narg(next_retry_at)
)
RETURNING id, subscription_id, revision_id, event_type, attempt_no, status, response_code, error, next_retry_at, created_at, updated_at;

-- name: UpdateDeliveryAttemptResult :one
UPDATE delivery_attempts
SET status = sqlc.arg(status),
    response_code = sqlc.narg(response_code),
    error = sqlc.arg(error),
    next_retry_at = sqlc.narg(next_retry_at),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING id, subscription_id, revision_id, event_type, attempt_no, status, response_code, error, next_retry_at, created_at, updated_at;

-- name: ListDueDeliveryAttempts :many
SELECT id, subscription_id, revision_id, event_type, attempt_no, status, response_code, error, next_retry_at, created_at, updated_at
FROM delivery_attempts
WHERE status IN ('pending', 'retry_scheduled')
  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
ORDER BY created_at
LIMIT sqlc.arg(limit_count);
