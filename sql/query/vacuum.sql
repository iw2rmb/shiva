-- name: ListVacuumRules :many
SELECT rule_id, severity, type, category_id, category_name, description, how_to_fix, given_path, rule_json
FROM vacuum_rules
ORDER BY rule_id ASC;

-- name: EnsureVacuumRule :exec
WITH rule_input AS (
    SELECT sqlc.arg(rule_id)::TEXT AS rule_id
)
INSERT INTO vacuum_rules (
    rule_id,
    severity,
    type,
    category_id,
    category_name,
    description,
    how_to_fix,
    given_path,
    rule_json
)
SELECT
    rule_input.rule_id,
    'warn',
    'validation',
    'validation',
    'Validation',
    'Rule metadata is not present in Shiva seed; issue was recorded with fallback metadata.',
    'Update Shiva vacuum rule seed to include this rule identifier.',
    '$',
    jsonb_build_object('id', rule_input.rule_id, 'generated_by', 'shiva-fallback')
FROM rule_input
ON CONFLICT (rule_id) DO NOTHING;

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
