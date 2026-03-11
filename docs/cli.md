# CLI

## Scope
This document describes the shipped draft `shiva` CLI.

## Command Surface
- `shiva <repo-path>`
  - prints the canonical OpenAPI document as YAML.
- `shiva <repo-path>#<operationId>`
  - prints the matched operation as a JSON operation slice:
    `{"paths":{"<path>":{"<method>":<operation-object>}}}`
- `shiva completion bash|zsh|fish|powershell`
  - prints a static shell completion script.

## Zsh Installation
- Generate the completion file with:
  - `shiva completion zsh > ~/.zsh_completions/_shiva`
- The completion file should be named `_shiva`, not `_shiva.sh`.
- `~/.zsh_completions` must be present in `fpath` before `compinit` runs.
- Reload completion after install, for example:
  - `autoload -Uz compinit && compinit`
- The generated script registers completion for the command name `shiva`.
- Invoking the binary as `./shiva` does not use the generated `shiva` completion binding.

## Resolution Rules
- `repo-path` is the GitLab `path_with_namespace`, for example `allure/allure-deployment`.
- The draft CLI resolves the repo snapshot through `GET /v1/specs/{repo}/apis`.
- The repo must have exactly one active API root on the default-branch latest processed snapshot.
- Repos with zero active API roots return not found.
- Repos with multiple active API roots return an ambiguity error that lists candidate API roots.
- After API resolution, the CLI fetches the API-scoped canonical spec from
  `GET /v1/specs/{repo}/-/{api}/-/openapi.{yaml|json}`.
- `#operationId` lookup is exact-match and case-sensitive.
- `#operationId` resolution is client-side in the draft CLI: it scans the canonical spec JSON with the same endpoint extraction rules used during ingestion.

## Configuration
- `SHIVA_BASE_URL`
  - Shiva HTTP base URL for the CLI.
  - default: `http://127.0.0.1:8080`
- `SHIVA_REQUEST_TIMEOUT_SECONDS`
  - per-request timeout for draft CLI reads.
  - default: `10`

## Output and Exit Codes
- Successful command output is written to stdout.
- Errors are written to stderr.
- Exit codes:
  - `0`: success
  - `2`: invalid input or ambiguous repo/operation resolution
  - `3`: not found
  - `4`: conflict
  - `10`: transport failure
  - `11`: internal client or server error

## Current Limits
- The draft CLI is read-only.
- The draft CLI supports only repo-level spec fetch and `#operationId` lookup.
- The draft CLI does not support explicit API selection, revision selection, `(method, path)` lookup, or request execution.
- Generated completion scripts are static; current completion covers only the stable command tree and shell-name arguments.
- Dynamic repo/API/operation completions are not shipped yet.
- The draft CLI does not keep a local catalog or refresh cache.

## Endpoint and Completion Decisions
- No new read endpoint was required to ship the draft CLI.
- The draft CLI reuses the current API listing route plus API-scoped spec fetch routes.
- Query-style read endpoints are not shipped yet:
  - `/v1/spec`
  - `/v1/operation`
  - `/v1/apis`
  - `/v1/operations`
  - `/v1/repos`
  - `/v1/catalog/status`
- Dedicated query-style inventory or freshness endpoints become necessary when the CLI starts doing dynamic repo/API/operation completion or local catalog refresh.
- Static completion generation is correct to ship now because the command tree is stable.
- Dynamic completions should be added only after the CLI has a local catalog plus cheap server-side inventory/freshness reads.

## References
- Runtime configuration: `docs/setup.md`
- HTTP route contract: `docs/endpoints.md`
- Test coverage and commands: `docs/testing.md`
