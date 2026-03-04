-- name: CreateSubscription :one
INSERT INTO subscriptions (
    tenant_id,
    repo_id,
    target_url,
    secret,
    enabled,
    max_attempts,
    backoff_initial_seconds,
    backoff_max_seconds
)
VALUES (
    sqlc.arg(tenant_id),
    sqlc.arg(repo_id),
    sqlc.arg(target_url),
    sqlc.arg(secret),
    sqlc.arg(enabled),
    sqlc.arg(max_attempts),
    sqlc.arg(backoff_initial_seconds),
    sqlc.arg(backoff_max_seconds)
)
RETURNING id, tenant_id, repo_id, target_url, secret, enabled, max_attempts, backoff_initial_seconds, backoff_max_seconds, created_at, updated_at;

-- name: ListEnabledSubscriptionsByRepo :many
SELECT id, tenant_id, repo_id, target_url, secret, enabled, max_attempts, backoff_initial_seconds, backoff_max_seconds, created_at, updated_at
FROM subscriptions
WHERE tenant_id = sqlc.arg(tenant_id)
  AND repo_id = sqlc.arg(repo_id)
  AND enabled = TRUE
ORDER BY id;

-- name: SetSubscriptionEnabled :one
UPDATE subscriptions
SET enabled = sqlc.arg(enabled),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING id, tenant_id, repo_id, target_url, secret, enabled, max_attempts, backoff_initial_seconds, backoff_max_seconds, created_at, updated_at;
