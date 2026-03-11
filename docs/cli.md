# CLI

## Scope
This document describes the current shipped `shiva` CLI surface, selector grammar, call execution behavior, request-envelope pipelines, and remaining gaps.

## Command Surface
- Root shorthand:
  - `shiva <repo-ref>`
  - `shiva <repo-ref>#<operationId>`
  - `shiva <repo-ref> <method> <path>`
  - `shiva <repo-ref>@<target>#<operationId>`
  - `shiva <repo-ref>@<target> <method> <path>`
- Explicit subcommands:
  - `shiva completion bash|zsh|fish|powershell`
  - `shiva health`
  - `shiva ls repos`
  - `shiva ls apis <repo-ref>`
  - `shiva ls ops <repo-ref>`
  - `shiva sync <repo-ref>`
  - `shiva batch`

`completion`, `health`, `ls`, `sync`, and `batch` are all implemented.

## Selector Grammar
- `repo-ref` uses the raw GitLab `path_with_namespace`, for example `allure/allure-deployment`.
- Packed selectors support:
  - `<repo-ref>`
  - `<repo-ref>#<operationId>`
  - `<repo-ref>@<target>#<operationId>`
- `@<target>` is equivalent to `--via <target>`.
- If both packed `@<target>` and `--via <target>` are present, they must match.
- `#<operationId>` must stay in the same shell token as the repo selector.
- Method/path lookup uses the separate positional form:
  - `shiva <repo-ref> <method> <path>`
- CLI paths normalize `:param` segments into OpenAPI `{param}` before lookup:
  - `/pets/:id` becomes `/pets/{id}`.
- Method lookup normalizes method names to lowercase before lookup.
- `--api <root-path>`, `--sha <sha8>`, and `--rev <revision-id>` apply to spec, operation, and call shorthand.
- `--rev` and `--sha` are mutually exclusive.
- `--dry-run` is valid only in call mode.
- `--refresh` and `--offline` are mutually exclusive.

## Call Input Flags
- `--path key=value`
- `--query key=value`
- `--header Name=value`
- `--json <inline-json|@file>`
- `--body @file`

Rules:
- `--path` keys must be unique.
- `--query` and `--header` may be repeated.
- `--json` and `--body` are mutually exclusive.
- `--body` accepts only `@file`.

## Transport and Execution
- Spec fetch uses `GET /v1/spec`.
- Operation fetch uses `GET /v1/operation`.
- `@shiva` calls use `POST /v1/call`.
- Direct targets resolve the operation from cached catalog data and dispatch the final HTTP request from the CLI process.
- `shiva health` uses `GET /healthz`.
- The CLI no longer resolves `operationId` by downloading a full spec and scanning it client-side.
- Packed and positional shorthand are normalized into the shared request-envelope model before execution.

## Output
- `shiva <repo-ref>`
  - default output: `yaml`
  - supported `-o/--output`: `yaml`, `json`
- `shiva <repo-ref>#<operationId>` and `shiva <repo-ref> <method> <path>`
  - default output: `json`
  - supported `-o/--output`: `json`, `yaml`
  - YAML output is rendered client-side from the canonical JSON operation body
- Call shorthand
  - default output: `body`
  - supported `-o/--output`: `body`, `json`
  - `--dry-run` switches call output to the execution plan
  - dry-run output modes: `json`, `curl`
  - `curl` dry-run output is supported only for direct targets
- `shiva ls repos`, `shiva ls apis`, `shiva ls ops`
  - supported `-o/--output`: `table`, `tsv`, `json`, `ndjson`
  - default output: `table` on TTY, `ndjson` otherwise
  - `ls ops` rows include repo, API, method, path, operation id, summary, and deprecated state
  - `--emit request` emits executable request envelopes as NDJSON instead of row output
  - `ls ops --emit request --via <target>` emits call envelopes instead of inspect envelopes
- `shiva sync <repo-ref>`
  - output: JSON summary with repo, cache scope, resolved snapshot revision when known, API count, and refreshed operation-catalog count
- `shiva batch`
  - reads request envelopes from stdin or `--from <file>`
  - default output: `ndjson`
  - supported `-o/--output`: `ndjson`, `json`
  - emits result rows with `index`, `request`, `ok`, `output`, and `error`

Success writes to stdout. Errors write to stderr.

## Request Envelopes
- `ls --emit request` and `batch` share the same JSON envelope model.
- `batch` accepts:
  - `{"kind":"spec", ...}`
  - `{"kind":"operation", ...}`
  - `{"kind":"call", ...}`
- `batch --dry-run` applies only to call envelopes.
- `batch` executes spec envelopes as JSON spec fetches, operation envelopes as JSON operation fetches, and call envelopes with JSON call output.

## Configuration
- Source profiles and execution targets are loaded from `$XDG_CONFIG_HOME/shiva/profiles.yaml` or `~/.config/shiva/profiles.yaml`.
- If no config file exists, the CLI synthesizes one default source profile from the environment-backed fallback below.
- The config file supports:
  - top-level `active_profile`
  - `profiles.<name>.base_url`
  - `profiles.<name>.token` or `profiles.<name>.token_env`
  - `profiles.<name>.timeout`
  - `targets.<name>.mode`
  - `targets.<name>.source_profile`
  - `targets.<name>.base_url`, `token`, `token_env`, and `timeout` for `direct` targets
- A built-in `shiva` target always exists even when it is not declared in the config file.
- `--profile <name>` overrides the active profile.
- A configured target may override the source profile used for selector resolution.
- `SHIVA_BASE_URL`
  - fallback base URL used only when `~/.config/shiva/profiles.yaml` is absent
  - default: `http://127.0.0.1:8080`
- `SHIVA_REQUEST_TIMEOUT_SECONDS`
  - fallback timeout used only when `~/.config/shiva/profiles.yaml` is absent
  - default: `10`

## Catalog Cache
- Catalog data is cached under `$XDG_CACHE_HOME/shiva/catalog/v1` or `~/.cache/shiva/catalog/v1`.
- The cache stores:
  - repo inventory from `/v1/repos`
  - default-branch freshness rows from `/v1/catalog/status`
  - API inventory from `/v1/apis`
  - operation inventory from `/v1/operations`
  - explicit spec and operation responses for offline reuse
- Floating selectors without `--sha` or `--rev` refresh lazily from `/v1/catalog/status` before refreshing catalog slices.
- Pinned `--sha` and `--rev` selectors reuse immutable cache entries.
- `--refresh` forces network refresh.
- `--offline` forbids network refreshes and serves only cached catalog and explicit response data.
- Repeated `--refresh` work against the same repo/API/scope is coalesced within one CLI invocation, including `batch`.

## Exit Codes
- `0`: success
- `2`: invalid input or ambiguous selector
- `3`: not found
- `4`: conflict
- `10`: transport failure
- `11`: internal client or server error

## Current Limits
- Dynamic repo/API/operation/profile/target and HTTP-method completions are shipped through the generated Cobra completion script.
- Completion reads from the local catalog cache first and may do a short best-effort refresh for stale slices before falling back to cached values.
- `ls repos --emit request` emits only repos with exactly one active API snapshot, because repo-only spec fetch remains ambiguous for multi-API repos.

## Ambiguity Reporting
- Multi-API spec ambiguity is surfaced as CLI invalid-input output with candidate API rows from the query transport.
- Duplicate operation-id and method/path ambiguity is surfaced as CLI invalid-input output with candidate API, method, path, and operation id data from the query transport.

## Zsh Installation
- Generate the completion file with:
  - `shiva completion zsh > ~/.zsh_completions/_shiva`
- The completion file should be named `_shiva`, not `_shiva.sh`.
- `~/.zsh_completions` must be present in `fpath` before `compinit` runs.
- Reload completion after install, for example:
  - `autoload -Uz compinit && compinit`
- The generated script registers completion for the command name `shiva`.
- Invoking the binary as `./shiva` does not use the generated `shiva` completion binding.

## References
- Endpoint transport contract: `docs/endpoints.md`
- Runtime configuration: `docs/setup.md`
- Test coverage and commands: `docs/testing.md`
