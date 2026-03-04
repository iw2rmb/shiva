-- name: DeleteEndpointIndexByRevision :exec
DELETE FROM endpoint_index
WHERE revision_id = sqlc.arg(revision_id);

-- name: InsertEndpointIndex :one
INSERT INTO endpoint_index (
    revision_id,
    method,
    path,
    operation_id,
    summary,
    deprecated,
    raw_json
)
VALUES (
    sqlc.arg(revision_id),
    sqlc.arg(method),
    sqlc.arg(path),
    sqlc.narg(operation_id),
    sqlc.narg(summary),
    sqlc.arg(deprecated),
    sqlc.arg(raw_json)
)
RETURNING id, revision_id, method, path, operation_id, summary, deprecated, raw_json, created_at;

-- name: ListEndpointIndexByRevision :many
SELECT id, revision_id, method, path, operation_id, summary, deprecated, raw_json, created_at
FROM endpoint_index
WHERE revision_id = sqlc.arg(revision_id)
ORDER BY method, path;

-- name: GetEndpointByMethodPath :one
SELECT id, revision_id, method, path, operation_id, summary, deprecated, raw_json, created_at
FROM endpoint_index
WHERE revision_id = sqlc.arg(revision_id)
  AND method = sqlc.arg(method)
  AND path = sqlc.arg(path);
