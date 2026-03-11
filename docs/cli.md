# CLI

## Scope
This document describes the current shipped `shiva` CLI surface, the selector grammar it accepts today, and the gaps that remain before the full CLI design is complete.

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

`completion`, `health`, `ls`, and `sync` are implemented end-to-end. `batch` is still a placeholder until the later CLI roadmap item lands.

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
- CLI paths normalize `:param` segments into OpenAPI `{param}` before transport lookup:
  - `/pets/:id` becomes `/pets/{id}`.
- Method lookup normalizes method names to lowercase before transport lookup.
- `--api <root-path>`, `--sha <sha8>`, and `--rev <revision-id>` apply to spec, operation, and call-plan shorthand.
- `--rev` and `--sha` are mutually exclusive.
- `--dry-run` is valid only in call mode.
- `--refresh` and `--offline` are mutually exclusive.

## Transport Behavior
- Spec fetch uses `GET /v1/spec`.
- Operation fetch uses `GET /v1/operation`.
- Call shorthand uses `POST /v1/call`.
- `shiva health` uses `GET /healthz`.
- The CLI no longer resolves `operationId` by downloading a full spec and scanning it client-side.
- Packed and positional shorthand are normalized into the shared request-envelope model before the HTTP request is built.

## Output
- `shiva <repo-ref>`
  - default output: YAML
  - supported `-o/--output`: `yaml`, `json`
- `shiva <repo-ref>#<operationId>` and `shiva <repo-ref> <method> <path>`
  - default output: JSON
  - supported `-o/--output`: `json`, `yaml`
  - YAML output is rendered client-side from the canonical JSON operation body
- Call shorthand
  - current output: normalized call-plan JSON from `POST /v1/call`
  - supported `-o/--output`: `json`
  - `--dry-run` is forwarded into the call envelope, but the current backend remains planning-only and does not dispatch upstream requests
- `shiva ls repos`, `shiva ls apis`, `shiva ls ops`
  - supported `-o/--output`: `table`, `tsv`, `json`, `ndjson`
  - default output: `table` on TTY, `ndjson` otherwise
  - `ls ops` rows include repo, API, method, path, operation id, summary, and deprecated state
- `shiva sync <repo-ref>`
  - output: JSON summary with repo, cache scope, resolved snapshot revision when known, API count, and refreshed operation-catalog count
- Success writes to stdout.
- Errors write to stderr.

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
  - explicit spec, operation, and call-plan responses for offline reuse
- Floating selectors without `--sha` or `--rev` refresh lazily from `/v1/catalog/status` before refreshing catalog slices.
- Pinned `--sha` and `--rev` selectors reuse immutable cache entries.
- `--refresh` forces catalog refresh before the explicit spec/operation/call request.
- `--offline` forbids network refreshes and serves only cached catalog and explicit response data.

## Exit Codes
- `0`: success
- `2`: invalid input or ambiguous selector
- `3`: not found
- `4`: conflict
- `10`: transport failure
- `11`: internal client or server error

## Current Limits
- Call shorthand currently exposes Shiva call planning only. Direct-target execution, final request dispatch, and dry-run executor formatting are not shipped yet.
- Dynamic repo/API/operation/profile/target completions are not shipped yet.
- `batch` is not shipped yet beyond its explicit command placeholder.

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
