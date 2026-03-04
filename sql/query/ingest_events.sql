-- name: CreateIngestEvent :one
INSERT INTO ingest_events (
    tenant_id,
    repo_id,
    event_type,
    delivery_id,
    payload_json
)
VALUES (
    sqlc.arg(tenant_id),
    sqlc.arg(repo_id),
    sqlc.arg(event_type),
    sqlc.arg(delivery_id),
    sqlc.arg(payload_json)
)
RETURNING id, tenant_id, repo_id, event_type, delivery_id, payload_json, received_at, status, error;

-- name: GetIngestEventByRepoDelivery :one
SELECT id, tenant_id, repo_id, event_type, delivery_id, payload_json, received_at, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND delivery_id = sqlc.arg(delivery_id);

-- name: UpdateIngestEventStatus :one
UPDATE ingest_events
SET status = sqlc.arg(status),
    error = sqlc.arg(error)
WHERE id = sqlc.arg(id)
RETURNING id, tenant_id, repo_id, event_type, delivery_id, payload_json, received_at, status, error;

-- name: ListPendingIngestEvents :many
SELECT id, tenant_id, repo_id, event_type, delivery_id, payload_json, received_at, status, error
FROM ingest_events
WHERE status = 'pending'
ORDER BY received_at
LIMIT sqlc.arg(limit_count);
