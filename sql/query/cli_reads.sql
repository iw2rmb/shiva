-- name: CountNamespaceCatalogInventory :one
WITH namespace_rows AS (
    SELECT
        namespaces.namespace,
        COUNT(*)::BIGINT AS repo_count,
        COALESCE(BOOL_AND(COALESCE(head.status IN ('pending', 'processing'), FALSE)), FALSE) AS all_pending
    FROM namespaces
    JOIN repos ON repos.namespace_id = namespaces.id
    LEFT JOIN LATERAL (
        SELECT status
        FROM ingest_events
        WHERE ingest_events.repo_id = repos.id
          AND ingest_events.branch = repos.default_branch
        ORDER BY ingest_events.received_at DESC, ingest_events.id DESC
        LIMIT 1
    ) AS head ON TRUE
    GROUP BY namespaces.namespace
),
filtered_rows AS (
    SELECT *
    FROM namespace_rows
    WHERE (sqlc.arg(query_prefix)::TEXT = '' OR namespace_rows.namespace ILIKE sqlc.arg(query_prefix)::TEXT || '%')
)
SELECT COUNT(*)::BIGINT AS total_count
FROM filtered_rows;

-- name: ListNamespaceCatalogInventory :many
WITH namespace_rows AS (
    SELECT
        namespaces.namespace,
        COUNT(*)::BIGINT AS repo_count,
        COALESCE(BOOL_AND(COALESCE(head.status IN ('pending', 'processing'), FALSE)), FALSE) AS all_pending
    FROM namespaces
    JOIN repos ON repos.namespace_id = namespaces.id
    LEFT JOIN LATERAL (
        SELECT status
        FROM ingest_events
        WHERE ingest_events.repo_id = repos.id
          AND ingest_events.branch = repos.default_branch
        ORDER BY ingest_events.received_at DESC, ingest_events.id DESC
        LIMIT 1
    ) AS head ON TRUE
    GROUP BY namespaces.namespace
),
filtered_rows AS (
    SELECT *
    FROM namespace_rows
    WHERE (sqlc.arg(query_prefix)::TEXT = '' OR namespace_rows.namespace ILIKE sqlc.arg(query_prefix)::TEXT || '%')
)
SELECT
    namespace,
    repo_count,
    all_pending
FROM filtered_rows
ORDER BY namespace ASC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- name: ListRepoCatalogInventory :many
SELECT
    repos.id,
    repos.gitlab_project_id,
    namespaces.namespace,
    repos.repo,
    repos.default_branch,
    repos.openapi_force_rescan,
    COALESCE(active_api_counts.active_api_count, 0)::BIGINT AS active_api_count,
    (head.id IS NOT NULL) AS head_present,
    COALESCE(head.id, 0)::BIGINT AS head_revision_id,
    COALESCE(head.sha, '')::TEXT AS head_revision_sha,
    COALESCE(head.status, '')::TEXT AS head_revision_status,
    head.openapi_changed AS head_revision_openapi_changed,
    COALESCE(head.received_at, TIMESTAMPTZ 'epoch') AS head_revision_received_at,
    head.processed_at AS head_revision_processed_at,
    (latest_openapi.id IS NOT NULL) AS snapshot_present,
    COALESCE(latest_openapi.id, 0)::BIGINT AS snapshot_revision_id,
    COALESCE(latest_openapi.sha, '')::TEXT AS snapshot_revision_sha,
    latest_openapi.processed_at AS snapshot_revision_processed_at
FROM repos
JOIN namespaces ON namespaces.id = repos.namespace_id
LEFT JOIN LATERAL (
    SELECT COUNT(*)::BIGINT AS active_api_count
    FROM api_specs
    WHERE api_specs.repo_id = repos.id
      AND api_specs.status = 'active'
) AS active_api_counts ON TRUE
LEFT JOIN LATERAL (
    SELECT id, sha, status, openapi_changed, received_at, processed_at
    FROM ingest_events
    WHERE ingest_events.repo_id = repos.id
      AND ingest_events.branch = repos.default_branch
    ORDER BY ingest_events.received_at DESC, ingest_events.id DESC
    LIMIT 1
) AS head ON TRUE
LEFT JOIN LATERAL (
    SELECT id, sha, processed_at
    FROM ingest_events
    WHERE ingest_events.repo_id = repos.id
      AND ingest_events.branch = repos.default_branch
      AND ingest_events.status = 'processed'
      AND ingest_events.openapi_changed = TRUE
    ORDER BY ingest_events.processed_at DESC NULLS LAST, ingest_events.id DESC
    LIMIT 1
) AS latest_openapi ON TRUE
ORDER BY namespaces.namespace ASC, repos.repo ASC;

-- name: GetRepoCatalogFreshness :one
SELECT
    repos.id,
    repos.gitlab_project_id,
    namespaces.namespace,
    repos.repo,
    repos.default_branch,
    repos.openapi_force_rescan,
    COALESCE(active_api_counts.active_api_count, 0)::BIGINT AS active_api_count,
    (head.id IS NOT NULL) AS head_present,
    COALESCE(head.id, 0)::BIGINT AS head_revision_id,
    COALESCE(head.sha, '')::TEXT AS head_revision_sha,
    COALESCE(head.status, '')::TEXT AS head_revision_status,
    head.openapi_changed AS head_revision_openapi_changed,
    COALESCE(head.received_at, TIMESTAMPTZ 'epoch') AS head_revision_received_at,
    head.processed_at AS head_revision_processed_at,
    (latest_openapi.id IS NOT NULL) AS snapshot_present,
    COALESCE(latest_openapi.id, 0)::BIGINT AS snapshot_revision_id,
    COALESCE(latest_openapi.sha, '')::TEXT AS snapshot_revision_sha,
    latest_openapi.processed_at AS snapshot_revision_processed_at
FROM repos
JOIN namespaces ON namespaces.id = repos.namespace_id
LEFT JOIN LATERAL (
    SELECT COUNT(*)::BIGINT AS active_api_count
    FROM api_specs
    WHERE api_specs.repo_id = repos.id
      AND api_specs.status = 'active'
) AS active_api_counts ON TRUE
LEFT JOIN LATERAL (
    SELECT id, sha, status, openapi_changed, received_at, processed_at
    FROM ingest_events
    WHERE ingest_events.repo_id = repos.id
      AND ingest_events.branch = repos.default_branch
    ORDER BY ingest_events.received_at DESC, ingest_events.id DESC
    LIMIT 1
) AS head ON TRUE
LEFT JOIN LATERAL (
    SELECT id, sha, processed_at
    FROM ingest_events
    WHERE ingest_events.repo_id = repos.id
      AND ingest_events.branch = repos.default_branch
      AND ingest_events.status = 'processed'
      AND ingest_events.openapi_changed = TRUE
    ORDER BY ingest_events.processed_at DESC NULLS LAST, ingest_events.id DESC
    LIMIT 1
) AS latest_openapi ON TRUE
WHERE namespaces.namespace = sqlc.arg(namespace)
  AND repos.repo = sqlc.arg(repo);

-- name: ListAPISnapshotInventoryByRepoRevision :many
WITH RECURSIVE snapshot_ancestors AS (
    SELECT id, repo_id, sha, parent_sha, 0::BIGINT AS distance
    FROM ingest_events
    WHERE ingest_events.repo_id = sqlc.arg(repo_id)
      AND ingest_events.id = sqlc.arg(snapshot_revision_id)
    UNION ALL
    SELECT parent.id, parent.repo_id, parent.sha, parent.parent_sha, snapshot_ancestors.distance + 1
    FROM ingest_events AS parent
    JOIN snapshot_ancestors
      ON parent.repo_id = snapshot_ancestors.repo_id
     AND parent.sha = snapshot_ancestors.parent_sha
),
repo_specs AS (
    SELECT id, root_path, status, display_name
    FROM api_specs
    WHERE api_specs.repo_id = sqlc.arg(repo_id)
),
latest_processed AS (
    SELECT DISTINCT ON (api_spec_revisions.api_spec_id)
        api_spec_revisions.api_spec_id,
        api_spec_revisions.id AS api_spec_revision_id,
        api_spec_revisions.ingest_event_id
    FROM api_spec_revisions
    JOIN repo_specs ON repo_specs.id = api_spec_revisions.api_spec_id
    JOIN snapshot_ancestors ON snapshot_ancestors.id = api_spec_revisions.ingest_event_id
    WHERE api_spec_revisions.build_status = 'processed'
    ORDER BY api_spec_revisions.api_spec_id, snapshot_ancestors.distance ASC, api_spec_revisions.id DESC
)
SELECT
    repo_specs.id AS api_spec_id,
    repo_specs.root_path AS api,
    repo_specs.status,
    repo_specs.display_name,
    latest_processed.api_spec_revision_id,
    latest_processed.ingest_event_id,
    ingest_events.sha AS ingest_event_sha,
    ingest_events.branch AS ingest_event_branch,
    spec_artifacts.etag AS spec_etag,
    spec_artifacts.size_bytes AS spec_size_bytes,
    COALESCE(operation_counts.operation_count, 0)::BIGINT AS operation_count
FROM repo_specs
LEFT JOIN latest_processed ON latest_processed.api_spec_id = repo_specs.id
LEFT JOIN ingest_events ON ingest_events.id = latest_processed.ingest_event_id
LEFT JOIN spec_artifacts ON spec_artifacts.api_spec_revision_id = latest_processed.api_spec_revision_id
LEFT JOIN LATERAL (
    SELECT COUNT(*)::BIGINT AS operation_count
    FROM endpoint_index
    WHERE endpoint_index.api_spec_revision_id = latest_processed.api_spec_revision_id
) AS operation_counts ON TRUE
ORDER BY repo_specs.root_path ASC;

-- name: GetAPISnapshotByRepoRevisionAndAPI :one
WITH RECURSIVE snapshot_ancestors AS (
    SELECT id, repo_id, sha, parent_sha, 0::BIGINT AS distance
    FROM ingest_events
    WHERE ingest_events.repo_id = sqlc.arg(repo_id)
      AND ingest_events.id = sqlc.arg(snapshot_revision_id)
    UNION ALL
    SELECT parent.id, parent.repo_id, parent.sha, parent.parent_sha, snapshot_ancestors.distance + 1
    FROM ingest_events AS parent
    JOIN snapshot_ancestors
      ON parent.repo_id = snapshot_ancestors.repo_id
     AND parent.sha = snapshot_ancestors.parent_sha
),
repo_specs AS (
    SELECT id, root_path, status, display_name
    FROM api_specs
    WHERE api_specs.repo_id = sqlc.arg(repo_id)
      AND api_specs.root_path = sqlc.arg(api)
),
latest_processed AS (
    SELECT DISTINCT ON (api_spec_revisions.api_spec_id)
        api_spec_revisions.api_spec_id,
        api_spec_revisions.id AS api_spec_revision_id,
        api_spec_revisions.ingest_event_id
    FROM api_spec_revisions
    JOIN repo_specs ON repo_specs.id = api_spec_revisions.api_spec_id
    JOIN snapshot_ancestors ON snapshot_ancestors.id = api_spec_revisions.ingest_event_id
    WHERE api_spec_revisions.build_status = 'processed'
    ORDER BY api_spec_revisions.api_spec_id, snapshot_ancestors.distance ASC, api_spec_revisions.id DESC
)
SELECT
    repo_specs.id AS api_spec_id,
    repo_specs.root_path AS api,
    repo_specs.status,
    repo_specs.display_name,
    latest_processed.api_spec_revision_id,
    latest_processed.ingest_event_id,
    ingest_events.sha AS ingest_event_sha,
    ingest_events.branch AS ingest_event_branch,
    spec_artifacts.etag AS spec_etag,
    spec_artifacts.size_bytes AS spec_size_bytes,
    COALESCE(operation_counts.operation_count, 0)::BIGINT AS operation_count
FROM repo_specs
LEFT JOIN latest_processed ON latest_processed.api_spec_id = repo_specs.id
LEFT JOIN ingest_events ON ingest_events.id = latest_processed.ingest_event_id
LEFT JOIN spec_artifacts ON spec_artifacts.api_spec_revision_id = latest_processed.api_spec_revision_id
LEFT JOIN LATERAL (
    SELECT COUNT(*)::BIGINT AS operation_count
    FROM endpoint_index
    WHERE endpoint_index.api_spec_revision_id = latest_processed.api_spec_revision_id
) AS operation_counts ON TRUE;

-- name: ListOperationInventoryByRepoRevision :many
WITH RECURSIVE snapshot_ancestors AS (
    SELECT id, repo_id, sha, parent_sha, 0::BIGINT AS distance
    FROM ingest_events
    WHERE ingest_events.repo_id = sqlc.arg(repo_id)
      AND ingest_events.id = sqlc.arg(snapshot_revision_id)
    UNION ALL
    SELECT parent.id, parent.repo_id, parent.sha, parent.parent_sha, snapshot_ancestors.distance + 1
    FROM ingest_events AS parent
    JOIN snapshot_ancestors
      ON parent.repo_id = snapshot_ancestors.repo_id
     AND parent.sha = snapshot_ancestors.parent_sha
),
repo_specs AS (
    SELECT id, root_path, status
    FROM api_specs
    WHERE api_specs.repo_id = sqlc.arg(repo_id)
),
latest_processed AS (
    SELECT DISTINCT ON (api_spec_revisions.api_spec_id)
        api_spec_revisions.api_spec_id,
        api_spec_revisions.id AS api_spec_revision_id,
        api_spec_revisions.ingest_event_id
    FROM api_spec_revisions
    JOIN repo_specs ON repo_specs.id = api_spec_revisions.api_spec_id
    JOIN snapshot_ancestors ON snapshot_ancestors.id = api_spec_revisions.ingest_event_id
    WHERE api_spec_revisions.build_status = 'processed'
    ORDER BY api_spec_revisions.api_spec_id, snapshot_ancestors.distance ASC, api_spec_revisions.id DESC
)
SELECT
    repo_specs.id AS api_spec_id,
    repo_specs.root_path AS api,
    repo_specs.status,
    latest_processed.api_spec_revision_id,
    latest_processed.ingest_event_id,
    ingest_events.sha AS ingest_event_sha,
    ingest_events.branch AS ingest_event_branch,
    endpoint_index.method,
    endpoint_index.path,
    endpoint_index.operation_id,
    endpoint_index.summary,
    endpoint_index.deprecated,
    endpoint_index.raw_json
FROM latest_processed
JOIN repo_specs ON repo_specs.id = latest_processed.api_spec_id
JOIN ingest_events ON ingest_events.id = latest_processed.ingest_event_id
JOIN endpoint_index ON endpoint_index.api_spec_revision_id = latest_processed.api_spec_revision_id
ORDER BY repo_specs.root_path ASC, endpoint_index.method ASC, endpoint_index.path ASC;

-- name: ListOperationInventoryByRepoRevisionAndAPI :many
WITH RECURSIVE snapshot_ancestors AS (
    SELECT id, repo_id, sha, parent_sha, 0::BIGINT AS distance
    FROM ingest_events
    WHERE ingest_events.repo_id = sqlc.arg(repo_id)
      AND ingest_events.id = sqlc.arg(snapshot_revision_id)
    UNION ALL
    SELECT parent.id, parent.repo_id, parent.sha, parent.parent_sha, snapshot_ancestors.distance + 1
    FROM ingest_events AS parent
    JOIN snapshot_ancestors
      ON parent.repo_id = snapshot_ancestors.repo_id
     AND parent.sha = snapshot_ancestors.parent_sha
),
repo_specs AS (
    SELECT id, root_path, status
    FROM api_specs
    WHERE api_specs.repo_id = sqlc.arg(repo_id)
      AND api_specs.root_path = sqlc.arg(api)
),
latest_processed AS (
    SELECT DISTINCT ON (api_spec_revisions.api_spec_id)
        api_spec_revisions.api_spec_id,
        api_spec_revisions.id AS api_spec_revision_id,
        api_spec_revisions.ingest_event_id
    FROM api_spec_revisions
    JOIN repo_specs ON repo_specs.id = api_spec_revisions.api_spec_id
    JOIN snapshot_ancestors ON snapshot_ancestors.id = api_spec_revisions.ingest_event_id
    WHERE api_spec_revisions.build_status = 'processed'
    ORDER BY api_spec_revisions.api_spec_id, snapshot_ancestors.distance ASC, api_spec_revisions.id DESC
)
SELECT
    repo_specs.id AS api_spec_id,
    repo_specs.root_path AS api,
    repo_specs.status,
    latest_processed.api_spec_revision_id,
    latest_processed.ingest_event_id,
    ingest_events.sha AS ingest_event_sha,
    ingest_events.branch AS ingest_event_branch,
    endpoint_index.method,
    endpoint_index.path,
    endpoint_index.operation_id,
    endpoint_index.summary,
    endpoint_index.deprecated,
    endpoint_index.raw_json
FROM latest_processed
JOIN repo_specs ON repo_specs.id = latest_processed.api_spec_id
JOIN ingest_events ON ingest_events.id = latest_processed.ingest_event_id
JOIN endpoint_index ON endpoint_index.api_spec_revision_id = latest_processed.api_spec_revision_id
ORDER BY endpoint_index.method ASC, endpoint_index.path ASC;

-- name: FindOperationCandidatesByRepoRevisionAndOperationID :many
WITH RECURSIVE snapshot_ancestors AS (
    SELECT id, repo_id, sha, parent_sha, 0::BIGINT AS distance
    FROM ingest_events
    WHERE ingest_events.repo_id = sqlc.arg(repo_id)
      AND ingest_events.id = sqlc.arg(snapshot_revision_id)
    UNION ALL
    SELECT parent.id, parent.repo_id, parent.sha, parent.parent_sha, snapshot_ancestors.distance + 1
    FROM ingest_events AS parent
    JOIN snapshot_ancestors
      ON parent.repo_id = snapshot_ancestors.repo_id
     AND parent.sha = snapshot_ancestors.parent_sha
),
repo_specs AS (
    SELECT id, root_path, status
    FROM api_specs
    WHERE api_specs.repo_id = sqlc.arg(repo_id)
),
latest_processed AS (
    SELECT DISTINCT ON (api_spec_revisions.api_spec_id)
        api_spec_revisions.api_spec_id,
        api_spec_revisions.id AS api_spec_revision_id,
        api_spec_revisions.ingest_event_id
    FROM api_spec_revisions
    JOIN repo_specs ON repo_specs.id = api_spec_revisions.api_spec_id
    JOIN snapshot_ancestors ON snapshot_ancestors.id = api_spec_revisions.ingest_event_id
    WHERE api_spec_revisions.build_status = 'processed'
    ORDER BY api_spec_revisions.api_spec_id, snapshot_ancestors.distance ASC, api_spec_revisions.id DESC
)
SELECT
    repo_specs.id AS api_spec_id,
    repo_specs.root_path AS api,
    repo_specs.status,
    latest_processed.api_spec_revision_id,
    latest_processed.ingest_event_id,
    ingest_events.sha AS ingest_event_sha,
    ingest_events.branch AS ingest_event_branch,
    endpoint_index.method,
    endpoint_index.path,
    endpoint_index.operation_id,
    endpoint_index.summary,
    endpoint_index.deprecated,
    endpoint_index.raw_json
FROM latest_processed
JOIN repo_specs ON repo_specs.id = latest_processed.api_spec_id
JOIN ingest_events ON ingest_events.id = latest_processed.ingest_event_id
JOIN endpoint_index ON endpoint_index.api_spec_revision_id = latest_processed.api_spec_revision_id
WHERE endpoint_index.operation_id = sqlc.arg(operation_id)
ORDER BY repo_specs.root_path ASC, endpoint_index.method ASC, endpoint_index.path ASC;

-- name: FindOperationCandidatesByRepoRevisionAndAPIAndOperationID :many
WITH RECURSIVE snapshot_ancestors AS (
    SELECT id, repo_id, sha, parent_sha, 0::BIGINT AS distance
    FROM ingest_events
    WHERE ingest_events.repo_id = sqlc.arg(repo_id)
      AND ingest_events.id = sqlc.arg(snapshot_revision_id)
    UNION ALL
    SELECT parent.id, parent.repo_id, parent.sha, parent.parent_sha, snapshot_ancestors.distance + 1
    FROM ingest_events AS parent
    JOIN snapshot_ancestors
      ON parent.repo_id = snapshot_ancestors.repo_id
     AND parent.sha = snapshot_ancestors.parent_sha
),
repo_specs AS (
    SELECT id, root_path, status
    FROM api_specs
    WHERE api_specs.repo_id = sqlc.arg(repo_id)
      AND api_specs.root_path = sqlc.arg(api)
),
latest_processed AS (
    SELECT DISTINCT ON (api_spec_revisions.api_spec_id)
        api_spec_revisions.api_spec_id,
        api_spec_revisions.id AS api_spec_revision_id,
        api_spec_revisions.ingest_event_id
    FROM api_spec_revisions
    JOIN repo_specs ON repo_specs.id = api_spec_revisions.api_spec_id
    JOIN snapshot_ancestors ON snapshot_ancestors.id = api_spec_revisions.ingest_event_id
    WHERE api_spec_revisions.build_status = 'processed'
    ORDER BY api_spec_revisions.api_spec_id, snapshot_ancestors.distance ASC, api_spec_revisions.id DESC
)
SELECT
    repo_specs.id AS api_spec_id,
    repo_specs.root_path AS api,
    repo_specs.status,
    latest_processed.api_spec_revision_id,
    latest_processed.ingest_event_id,
    ingest_events.sha AS ingest_event_sha,
    ingest_events.branch AS ingest_event_branch,
    endpoint_index.method,
    endpoint_index.path,
    endpoint_index.operation_id,
    endpoint_index.summary,
    endpoint_index.deprecated,
    endpoint_index.raw_json
FROM latest_processed
JOIN repo_specs ON repo_specs.id = latest_processed.api_spec_id
JOIN ingest_events ON ingest_events.id = latest_processed.ingest_event_id
JOIN endpoint_index ON endpoint_index.api_spec_revision_id = latest_processed.api_spec_revision_id
WHERE endpoint_index.operation_id = sqlc.arg(operation_id)
ORDER BY endpoint_index.method ASC, endpoint_index.path ASC;

-- name: FindOperationCandidatesByRepoRevisionAndMethodPath :many
WITH RECURSIVE snapshot_ancestors AS (
    SELECT id, repo_id, sha, parent_sha, 0::BIGINT AS distance
    FROM ingest_events
    WHERE ingest_events.repo_id = sqlc.arg(repo_id)
      AND ingest_events.id = sqlc.arg(snapshot_revision_id)
    UNION ALL
    SELECT parent.id, parent.repo_id, parent.sha, parent.parent_sha, snapshot_ancestors.distance + 1
    FROM ingest_events AS parent
    JOIN snapshot_ancestors
      ON parent.repo_id = snapshot_ancestors.repo_id
     AND parent.sha = snapshot_ancestors.parent_sha
),
repo_specs AS (
    SELECT id, root_path, status
    FROM api_specs
    WHERE api_specs.repo_id = sqlc.arg(repo_id)
),
latest_processed AS (
    SELECT DISTINCT ON (api_spec_revisions.api_spec_id)
        api_spec_revisions.api_spec_id,
        api_spec_revisions.id AS api_spec_revision_id,
        api_spec_revisions.ingest_event_id
    FROM api_spec_revisions
    JOIN repo_specs ON repo_specs.id = api_spec_revisions.api_spec_id
    JOIN snapshot_ancestors ON snapshot_ancestors.id = api_spec_revisions.ingest_event_id
    WHERE api_spec_revisions.build_status = 'processed'
    ORDER BY api_spec_revisions.api_spec_id, snapshot_ancestors.distance ASC, api_spec_revisions.id DESC
)
SELECT
    repo_specs.id AS api_spec_id,
    repo_specs.root_path AS api,
    repo_specs.status,
    latest_processed.api_spec_revision_id,
    latest_processed.ingest_event_id,
    ingest_events.sha AS ingest_event_sha,
    ingest_events.branch AS ingest_event_branch,
    endpoint_index.method,
    endpoint_index.path,
    endpoint_index.operation_id,
    endpoint_index.summary,
    endpoint_index.deprecated,
    endpoint_index.raw_json
FROM latest_processed
JOIN repo_specs ON repo_specs.id = latest_processed.api_spec_id
JOIN ingest_events ON ingest_events.id = latest_processed.ingest_event_id
JOIN endpoint_index ON endpoint_index.api_spec_revision_id = latest_processed.api_spec_revision_id
WHERE endpoint_index.method = sqlc.arg(method)
  AND endpoint_index.path = sqlc.arg(path)
ORDER BY repo_specs.root_path ASC, endpoint_index.method ASC, endpoint_index.path ASC;

-- name: FindOperationCandidatesByRepoRevisionAndAPIAndMethodPath :many
WITH RECURSIVE snapshot_ancestors AS (
    SELECT id, repo_id, sha, parent_sha, 0::BIGINT AS distance
    FROM ingest_events
    WHERE ingest_events.repo_id = sqlc.arg(repo_id)
      AND ingest_events.id = sqlc.arg(snapshot_revision_id)
    UNION ALL
    SELECT parent.id, parent.repo_id, parent.sha, parent.parent_sha, snapshot_ancestors.distance + 1
    FROM ingest_events AS parent
    JOIN snapshot_ancestors
      ON parent.repo_id = snapshot_ancestors.repo_id
     AND parent.sha = snapshot_ancestors.parent_sha
),
repo_specs AS (
    SELECT id, root_path, status
    FROM api_specs
    WHERE api_specs.repo_id = sqlc.arg(repo_id)
      AND api_specs.root_path = sqlc.arg(api)
),
latest_processed AS (
    SELECT DISTINCT ON (api_spec_revisions.api_spec_id)
        api_spec_revisions.api_spec_id,
        api_spec_revisions.id AS api_spec_revision_id,
        api_spec_revisions.ingest_event_id
    FROM api_spec_revisions
    JOIN repo_specs ON repo_specs.id = api_spec_revisions.api_spec_id
    JOIN snapshot_ancestors ON snapshot_ancestors.id = api_spec_revisions.ingest_event_id
    WHERE api_spec_revisions.build_status = 'processed'
    ORDER BY api_spec_revisions.api_spec_id, snapshot_ancestors.distance ASC, api_spec_revisions.id DESC
)
SELECT
    repo_specs.id AS api_spec_id,
    repo_specs.root_path AS api,
    repo_specs.status,
    latest_processed.api_spec_revision_id,
    latest_processed.ingest_event_id,
    ingest_events.sha AS ingest_event_sha,
    ingest_events.branch AS ingest_event_branch,
    endpoint_index.method,
    endpoint_index.path,
    endpoint_index.operation_id,
    endpoint_index.summary,
    endpoint_index.deprecated,
    endpoint_index.raw_json
FROM latest_processed
JOIN repo_specs ON repo_specs.id = latest_processed.api_spec_id
JOIN ingest_events ON ingest_events.id = latest_processed.ingest_event_id
JOIN endpoint_index ON endpoint_index.api_spec_revision_id = latest_processed.api_spec_revision_id
WHERE endpoint_index.method = sqlc.arg(method)
  AND endpoint_index.path = sqlc.arg(path)
ORDER BY endpoint_index.method ASC, endpoint_index.path ASC;
