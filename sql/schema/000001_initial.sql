CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    checksum TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS repos (
    id BIGSERIAL PRIMARY KEY,
    gitlab_project_id BIGINT NOT NULL,
    path_with_namespace TEXT NOT NULL,
    default_branch TEXT NOT NULL,
    openapi_force_rescan BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (gitlab_project_id),
    UNIQUE (path_with_namespace)
);

CREATE TABLE IF NOT EXISTS startup_index_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    last_project_id BIGINT NOT NULL CHECK (last_project_id >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id BIGSERIAL PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    target_url TEXT NOT NULL,
    secret TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    max_attempts INTEGER NOT NULL DEFAULT 8 CHECK (max_attempts > 0),
    backoff_initial_seconds INTEGER NOT NULL DEFAULT 5 CHECK (backoff_initial_seconds > 0),
    backoff_max_seconds INTEGER NOT NULL DEFAULT 300 CHECK (backoff_max_seconds > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo_id, target_url)
);

CREATE INDEX IF NOT EXISTS subscriptions_repo_enabled_idx ON subscriptions(repo_id, enabled);

CREATE TABLE IF NOT EXISTS ingest_events (
    id BIGSERIAL PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    sha TEXT NOT NULL,
    branch TEXT NOT NULL,
    parent_sha TEXT,
    event_type TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    payload_json JSONB NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    openapi_changed BOOLEAN,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'processed', 'failed')),
    error TEXT NOT NULL DEFAULT '',
    UNIQUE (repo_id, delivery_id),
    UNIQUE (repo_id, sha)
);

CREATE INDEX IF NOT EXISTS ingest_events_status_retry_idx ON ingest_events(status, next_retry_at, id);
CREATE INDEX IF NOT EXISTS ingest_events_repo_id_idx ON ingest_events(repo_id, id);
CREATE INDEX IF NOT EXISTS ingest_events_repo_branch_processed_idx ON ingest_events(repo_id, branch, processed_at DESC);
CREATE INDEX IF NOT EXISTS ingest_events_repo_received_idx ON ingest_events(repo_id, received_at DESC);

CREATE TABLE IF NOT EXISTS api_specs (
    id BIGSERIAL PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    root_path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deleted')),
    display_name TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo_id, root_path)
);

CREATE INDEX IF NOT EXISTS api_specs_repo_status_idx ON api_specs(repo_id, status);

CREATE TABLE IF NOT EXISTS api_spec_revisions (
    id BIGSERIAL PRIMARY KEY,
    api_spec_id BIGINT NOT NULL REFERENCES api_specs(id) ON DELETE CASCADE,
    ingest_event_id BIGINT NOT NULL REFERENCES ingest_events(id) ON DELETE CASCADE,
    root_path_at_revision TEXT NOT NULL,
    build_status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (api_spec_id, ingest_event_id)
);

CREATE INDEX IF NOT EXISTS api_spec_revisions_ingest_event_id_idx ON api_spec_revisions(ingest_event_id);

CREATE TABLE IF NOT EXISTS api_spec_dependencies (
    api_spec_revision_id BIGINT NOT NULL REFERENCES api_spec_revisions(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    UNIQUE (api_spec_revision_id, file_path)
);

CREATE TABLE IF NOT EXISTS spec_artifacts (
    id BIGSERIAL PRIMARY KEY,
    api_spec_revision_id BIGINT NOT NULL UNIQUE REFERENCES api_spec_revisions(id) ON DELETE CASCADE,
    spec_json JSONB NOT NULL,
    spec_yaml TEXT NOT NULL,
    etag TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS endpoint_index (
    id BIGSERIAL PRIMARY KEY,
    api_spec_revision_id BIGINT NOT NULL REFERENCES api_spec_revisions(id) ON DELETE CASCADE,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    operation_id TEXT,
    summary TEXT,
    deprecated BOOLEAN NOT NULL DEFAULT FALSE,
    raw_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (api_spec_revision_id, method, path)
);

CREATE INDEX IF NOT EXISTS endpoint_index_api_spec_revision_idx ON endpoint_index(api_spec_revision_id);
CREATE INDEX IF NOT EXISTS endpoint_index_lookup_idx ON endpoint_index(api_spec_revision_id, method, path);

CREATE TABLE IF NOT EXISTS spec_changes (
    id BIGSERIAL PRIMARY KEY,
    api_spec_id BIGINT NOT NULL REFERENCES api_specs(id) ON DELETE CASCADE,
    from_api_spec_revision_id BIGINT REFERENCES api_spec_revisions(id) ON DELETE SET NULL,
    to_api_spec_revision_id BIGINT NOT NULL UNIQUE REFERENCES api_spec_revisions(id) ON DELETE CASCADE,
    change_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS spec_changes_api_spec_created_idx ON spec_changes(api_spec_id, created_at DESC);

CREATE TABLE IF NOT EXISTS delivery_attempts (
    id BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    api_spec_id BIGINT NOT NULL REFERENCES api_specs(id) ON DELETE CASCADE,
    ingest_event_id BIGINT NOT NULL REFERENCES ingest_events(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (event_type IN ('spec.updated.full', 'spec.updated.diff')),
    attempt_no INTEGER NOT NULL CHECK (attempt_no > 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'retry_scheduled', 'succeeded', 'failed')),
    response_code INTEGER,
    error TEXT NOT NULL DEFAULT '',
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (subscription_id, api_spec_id, ingest_event_id, event_type, attempt_no)
);

CREATE INDEX IF NOT EXISTS delivery_attempts_retry_idx ON delivery_attempts(status, next_retry_at);
CREATE INDEX IF NOT EXISTS delivery_attempts_subscription_idx ON delivery_attempts(subscription_id, created_at DESC);
