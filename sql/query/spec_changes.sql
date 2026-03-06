-- name: CreateSpecChange :one
INSERT INTO spec_changes (
    api_spec_id,
    from_api_spec_revision_id,
    to_api_spec_revision_id,
    change_json
)
VALUES (
    sqlc.arg(api_spec_id),
    sqlc.narg(from_api_spec_revision_id),
    sqlc.arg(to_api_spec_revision_id),
    sqlc.arg(change_json)
)
ON CONFLICT (to_api_spec_revision_id) DO UPDATE
SET api_spec_id = EXCLUDED.api_spec_id,
    from_api_spec_revision_id = EXCLUDED.from_api_spec_revision_id,
    change_json = EXCLUDED.change_json
RETURNING id, api_spec_id, from_api_spec_revision_id, to_api_spec_revision_id, change_json, created_at;

-- name: GetSpecChangeByToAPISpecRevision :one
SELECT id, api_spec_id, from_api_spec_revision_id, to_api_spec_revision_id, change_json, created_at
FROM spec_changes
WHERE to_api_spec_revision_id = sqlc.arg(to_api_spec_revision_id);
