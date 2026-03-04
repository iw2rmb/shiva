-- name: UpsertSpecArtifact :one
INSERT INTO spec_artifacts (
    revision_id,
    spec_json,
    spec_yaml,
    etag,
    size_bytes
)
VALUES (
    sqlc.arg(revision_id),
    sqlc.arg(spec_json),
    sqlc.arg(spec_yaml),
    sqlc.arg(etag),
    sqlc.arg(size_bytes)
)
ON CONFLICT (revision_id) DO UPDATE
SET spec_json = EXCLUDED.spec_json,
    spec_yaml = EXCLUDED.spec_yaml,
    etag = EXCLUDED.etag,
    size_bytes = EXCLUDED.size_bytes
RETURNING id, revision_id, spec_json, spec_yaml, etag, size_bytes, created_at;

-- name: GetSpecArtifactByRevisionID :one
SELECT id, revision_id, spec_json, spec_yaml, etag, size_bytes, created_at
FROM spec_artifacts
WHERE revision_id = sqlc.arg(revision_id);
