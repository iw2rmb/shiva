# Setup

## Scope
This document describes runtime setup, configuration, and startup behavior of the current Shiva codebase.

## Prerequisites
- Go `1.25+`.
- PostgreSQL.
- GitLab access token only if your GitLab APIs require auth.

## Run
- Start service:
  - `shivad`
- Run the CLI against a running Shiva instance:
  - `shiva allure/allure-deployment`
  - `shiva allure/allure-deployment#findAll_42`
  - `shiva allure/allure-deployment@prod#getUsers --path id=42`
  - `shiva allure/allure-deployment get /accessgroup/:id`
  - `shiva batch`
- Run tests:
  - `go test ./...`

## Runtime Behavior
- Shiva expects DB connectivity at startup.
- Missing DB URL or DB connection failure is a startup error.
- Shiva applies the embedded initial database schema at startup before any worker or startup-indexing queries run.
- Worker pipeline is always enabled when the service starts.
- Shiva launches startup indexing in the background on every service start.
- Startup indexing resumes from `startup_index_state.last_project_id`; when no checkpoint row exists it starts with GitLab `id_after=0`.
- Startup indexing paginates accessible GitLab projects, skips projects in personal (`user`) namespaces by default, resolves each remaining default-branch head SHA, enqueues synthetic ingest events into the normal DB-backed queue as pages are consumed, and advances the checkpoint after each fully handled project.
- Worker processing and startup indexing run independently, so processed canonical repo revisions can appear before startup indexing finishes.

## Environment Variables

### Core
- `SHIVA_HTTP_ADDR` (default `:8080`).
- `SHIVA_DATABASE_URL` (required).
- `SHIVA_LOG_LEVEL` (default `info`).
- `SHIVA_SHUTDOWN_TIMEOUT_SECONDS` (default `15`).

### GitLab + Ingest
- `SHIVA_GITLAB_BASE_URL` (required).
- `SHIVA_GITLAB_TOKEN` (optional, but startup indexing and repository processing need whatever access your GitLab APIs require).
- `SHIVA_GITLAB_WEBHOOK_SECRET` (required for accepting inbound GitLab webhooks).

### Worker + OpenAPI Resolver
- `SHIVA_WORKER_CONCURRENCY` (default `4`).
- `SHIVA_OPENAPI_PATH_GLOBS` (default: `**/openapi*.{yaml,yml,json},**/swagger*.{yaml,yml,json},**/api/**/*.yaml`).
- `SHIVA_OPENAPI_REF_MAX_FETCHES` (default `128`).
- `SHIVA_OPENAPI_BOOTSTRAP_FETCH_CONCURRENCY` (default `8`, must be `>= 1`).
- `SHIVA_OPENAPI_BOOTSTRAP_SNIFF_BYTES` (default `4096`, must be `>= 1`).

### Outbound Delivery
- `SHIVA_OUTBOUND_TIMEOUT_SECONDS` (default `10`).

### Ingress Controls
- `SHIVA_INGRESS_BODY_LIMIT_BYTES` (default `1048576`).
- `SHIVA_INGRESS_RATE_LIMIT_MAX` (default `120`).
- `SHIVA_INGRESS_RATE_LIMIT_WINDOW_SECONDS` (default `60`).

### Observability
- `SHIVA_METRICS_PATH` (default `/internal/metrics`).
- `SHIVA_TRACING_ENABLED` (default `true`).
- `SHIVA_TRACING_STDOUT` (default `false`).

### CLI Fallbacks
- `SHIVA_BASE_URL` (default `http://127.0.0.1:8080`).
- `SHIVA_REQUEST_TIMEOUT_SECONDS` (default `10`).

## CLI Runtime Files
- Config file:
  - `$XDG_CONFIG_HOME/shiva/profiles.yaml` or `~/.config/shiva/profiles.yaml`
- Cache root:
  - `$XDG_CACHE_HOME/shiva/catalog/v1` or `~/.cache/shiva/catalog/v1`

The CLI loads source profiles and execution targets from `profiles.yaml`.

Minimal profile shape:
- `base_url`
- `token` or `token_env` (optional today)
- `timeout`

Minimal target shape:
- `mode`
- `source_profile` (optional)
- `base_url`, `token`, `token_env`, `timeout` for `direct` targets

Example:

```yaml
active_profile: default
profiles:
  default:
    base_url: http://127.0.0.1:8080
    timeout: 10s
targets:
  prod:
    mode: direct
    base_url: https://api.example
    timeout: 10s
    source_profile: default
```

If `profiles.yaml` is absent, the CLI synthesizes one `default` profile from `SHIVA_BASE_URL` and `SHIVA_REQUEST_TIMEOUT_SECONDS`.

Current CLI cache behavior:
- floating selectors refresh repo/API/operation catalog slices lazily
- pinned `--sha` and `--rev` selectors reuse immutable cache entries
- `--offline` serves only cached catalog and explicit response data
- `shiva sync <repo-ref>` is the explicit repo refresh command

## Health and Metrics
- `GET /healthz`
  - returns service status and DB health (`ok`, `unreachable`).
- `GET /internal/metrics` (or configured metrics path).

## Deployment Reference
- Kubernetes deployment assets are maintained in the repository deployment manifests.
- Multi-arch GHCR publishing is supported by repository build automation.

## Container Image
- The final image contains only:
  - `/shivad` compiled binary
  - `/etc/ssl/certs/ca-certificates.crt` for outbound TLS validation
- Supported target platforms:
  - `linux/amd64`
  - `linux/arm64`
- Build and push are handled through the repository container-build helper.
- Common environment overrides:
  - `IMAGE_REPO`
  - `IMAGE_TAG`
  - `PUSH_LATEST`
- Optional auth from environment:
  - `GHCR_USERNAME=<github-username>`
  - `GHCR_TOKEN=<github-token-with-packages-write>`

## References
- CLI behavior: `docs/cli.md`
- GitLab ingest flow: `docs/gitlab.md`
- Endpoint/index and read routes: `docs/endpoints.md`
- Webhook contracts: `docs/webhooks.md`
- Database and sqlc: `docs/database.md`
- Test strategy and commands: `docs/testing.md`
