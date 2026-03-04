CREATE TABLE tenants (
    id BIGSERIAL PRIMARY KEY,
    key TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE repos (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    gitlab_project_id BIGINT NOT NULL,
    path_with_namespace TEXT NOT NULL,
    default_branch TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, gitlab_project_id),
    UNIQUE (tenant_id, path_with_namespace)
);

CREATE INDEX repos_tenant_id_idx ON repos(tenant_id);

CREATE TABLE subscriptions (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    repo_id BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    target_url TEXT NOT NULL,
    secret TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    max_attempts INTEGER NOT NULL DEFAULT 8 CHECK (max_attempts > 0),
    backoff_initial_seconds INTEGER NOT NULL DEFAULT 5 CHECK (backoff_initial_seconds > 0),
    backoff_max_seconds INTEGER NOT NULL DEFAULT 300 CHECK (backoff_max_seconds > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, repo_id, target_url),
    FOREIGN KEY (tenant_id, repo_id) REFERENCES repos(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX subscriptions_repo_enabled_idx ON subscriptions(repo_id, enabled);

CREATE TABLE ingest_events (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
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
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'processed', 'failed')),
    error TEXT NOT NULL DEFAULT '',
    UNIQUE (repo_id, delivery_id),
    UNIQUE (repo_id, sha),
    FOREIGN KEY (tenant_id, repo_id) REFERENCES repos(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX ingest_events_status_retry_idx ON ingest_events(status, next_retry_at, id);
CREATE INDEX ingest_events_repo_id_idx ON ingest_events(repo_id, id);

CREATE TABLE revisions (
    id BIGSERIAL PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    sha TEXT NOT NULL,
    branch TEXT NOT NULL,
    parent_sha TEXT,
    processed_at TIMESTAMPTZ,
    openapi_changed BOOLEAN,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processed', 'failed', 'skipped')),
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo_id, sha)
);

CREATE INDEX revisions_repo_branch_processed_idx ON revisions(repo_id, branch, processed_at DESC);
CREATE INDEX revisions_repo_created_idx ON revisions(repo_id, created_at DESC);

CREATE TABLE api_specs (
    id BIGSERIAL PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    root_path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deleted')),
    display_name TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo_id, root_path)
);

CREATE INDEX api_specs_repo_status_idx ON api_specs(repo_id, status);

CREATE TABLE api_spec_revisions (
    id BIGSERIAL PRIMARY KEY,
    api_spec_id BIGINT NOT NULL REFERENCES api_specs(id) ON DELETE CASCADE,
    revision_id BIGINT NOT NULL REFERENCES revisions(id) ON DELETE CASCADE,
    root_path_at_revision TEXT NOT NULL,
    build_status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (api_spec_id, revision_id)
);

CREATE INDEX api_spec_revisions_revision_id_idx ON api_spec_revisions(revision_id);

CREATE TABLE api_spec_dependencies (
    api_spec_revision_id BIGINT NOT NULL REFERENCES api_spec_revisions(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    UNIQUE (api_spec_revision_id, file_path)
);

CREATE TABLE spec_artifacts (
    id BIGSERIAL PRIMARY KEY,
    revision_id BIGINT NOT NULL UNIQUE REFERENCES revisions(id) ON DELETE CASCADE,
    spec_json JSONB NOT NULL,
    spec_yaml TEXT NOT NULL,
    etag TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE endpoint_index (
    id BIGSERIAL PRIMARY KEY,
    revision_id BIGINT NOT NULL REFERENCES revisions(id) ON DELETE CASCADE,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    operation_id TEXT,
    summary TEXT,
    deprecated BOOLEAN NOT NULL DEFAULT FALSE,
    raw_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (revision_id, method, path)
);

CREATE INDEX endpoint_index_revision_idx ON endpoint_index(revision_id);
CREATE INDEX endpoint_index_lookup_idx ON endpoint_index(revision_id, method, path);

CREATE TABLE spec_changes (
    id BIGSERIAL PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    from_revision_id BIGINT REFERENCES revisions(id) ON DELETE SET NULL,
    to_revision_id BIGINT NOT NULL UNIQUE REFERENCES revisions(id) ON DELETE CASCADE,
    change_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX spec_changes_repo_created_idx ON spec_changes(repo_id, created_at DESC);

CREATE TABLE delivery_attempts (
    id BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    revision_id BIGINT NOT NULL REFERENCES revisions(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (event_type IN ('spec.updated.full', 'spec.updated.diff')),
    attempt_no INTEGER NOT NULL CHECK (attempt_no > 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'retry_scheduled', 'succeeded', 'failed')),
    response_code INTEGER,
    error TEXT NOT NULL DEFAULT '',
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (subscription_id, revision_id, event_type, attempt_no)
);

CREATE INDEX delivery_attempts_retry_idx ON delivery_attempts(status, next_retry_at);
CREATE INDEX delivery_attempts_subscription_idx ON delivery_attempts(subscription_id, created_at DESC);
