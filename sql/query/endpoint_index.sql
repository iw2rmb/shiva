-- name: DeleteEndpointIndexByAPISpecRevision :exec
DELETE FROM endpoint_index
WHERE api_spec_revision_id = sqlc.arg(api_spec_revision_id);

-- name: InsertEndpointIndex :one
INSERT INTO endpoint_index (
    api_spec_revision_id,
    method,
    path,
    operation_id,
    summary,
    deprecated,
    raw_json
)
VALUES (
    sqlc.arg(api_spec_revision_id),
    sqlc.arg(method),
    sqlc.arg(path),
    sqlc.narg(operation_id),
    sqlc.narg(summary),
    sqlc.arg(deprecated),
    sqlc.arg(raw_json)
)
RETURNING id, api_spec_revision_id, method, path, operation_id, summary, deprecated, raw_json, created_at;

-- name: ListEndpointIndexByAPISpecRevision :many
SELECT id, api_spec_revision_id, method, path, operation_id, summary, deprecated, raw_json, created_at
FROM endpoint_index
WHERE api_spec_revision_id = sqlc.arg(api_spec_revision_id)
ORDER BY method, path;

-- name: GetEndpointByMethodPathForAPISpecRevision :one
SELECT id, api_spec_revision_id, method, path, operation_id, summary, deprecated, raw_json, created_at
FROM endpoint_index
WHERE api_spec_revision_id = sqlc.arg(api_spec_revision_id)
  AND method = sqlc.arg(method)
  AND path = sqlc.arg(path);
