-- name: GetStartupIndexLastProjectID :one
SELECT last_project_id
FROM startup_index_state
WHERE singleton = TRUE;

-- name: AdvanceStartupIndexLastProjectID :exec
INSERT INTO startup_index_state (
    singleton,
    last_project_id
)
VALUES (
    TRUE,
    sqlc.arg(last_project_id)
)
ON CONFLICT (singleton) DO UPDATE
SET last_project_id = GREATEST(startup_index_state.last_project_id, EXCLUDED.last_project_id),
    updated_at = NOW();
