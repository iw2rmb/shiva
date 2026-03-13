-- name: ListVacuumRules :many
SELECT rule_id, severity, type, category_id, category_name, description, how_to_fix, given_path, rule_json
FROM vacuum_rules
ORDER BY rule_id ASC;

-- name: CreateVacuumIssue :one
INSERT INTO vacuum_issues (
    api_spec_revision_id,
    rule_id,
    message,
    json_path,
    range_pos
)
VALUES (
    sqlc.arg(api_spec_revision_id),
    sqlc.arg(rule_id),
    sqlc.arg(message),
    sqlc.arg(json_path),
    sqlc.arg(range_pos)::INTEGER[]
)
RETURNING id, api_spec_revision_id, rule_id, message, json_path, range_pos, created_at;

-- name: ListVacuumIssuesByAPISpecRevisionID :many
SELECT id, api_spec_revision_id, rule_id, message, json_path, range_pos, created_at
FROM vacuum_issues
WHERE api_spec_revision_id = sqlc.arg(api_spec_revision_id)
ORDER BY id ASC;

-- name: DeleteVacuumIssuesByAPISpecRevisionID :exec
DELETE FROM vacuum_issues
WHERE api_spec_revision_id = sqlc.arg(api_spec_revision_id);

-- name: UpdateAPISpecRevisionVacuumState :one
UPDATE api_spec_revisions
SET vacuum_status = sqlc.arg(vacuum_status),
    vacuum_error = sqlc.arg(vacuum_error),
    vacuum_validated_at = sqlc.narg(vacuum_validated_at),
    updated_at = NOW()
WHERE id = sqlc.arg(api_spec_revision_id)
RETURNING
    id,
    api_spec_id,
    ingest_event_id,
    root_path_at_revision,
    build_status,
    error,
    vacuum_status,
    vacuum_error,
    vacuum_validated_at,
    created_at,
    updated_at;
