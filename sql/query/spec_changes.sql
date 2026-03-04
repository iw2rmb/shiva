-- name: CreateSpecChange :one
INSERT INTO spec_changes (
    repo_id,
    from_revision_id,
    to_revision_id,
    change_json
)
VALUES (
    sqlc.arg(repo_id),
    sqlc.narg(from_revision_id),
    sqlc.arg(to_revision_id),
    sqlc.arg(change_json)
)
RETURNING id, repo_id, from_revision_id, to_revision_id, change_json, created_at;

-- name: GetSpecChangeByToRevision :one
SELECT id, repo_id, from_revision_id, to_revision_id, change_json, created_at
FROM spec_changes
WHERE to_revision_id = sqlc.arg(to_revision_id);
