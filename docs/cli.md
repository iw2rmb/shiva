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
  - `shiva ls`
  - `shiva sync <repo-ref>`
  - `shiva batch`

`completion` and `health` are implemented end-to-end. `ls`, `sync`, and `batch` are explicit commands already, but they still return `not implemented yet` until later CLI roadmap items land.

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
- Success writes to stdout.
- Errors write to stderr.

## Configuration
- `SHIVA_BASE_URL`
  - Shiva HTTP base URL for the CLI
  - default: `http://127.0.0.1:8080`
- `SHIVA_REQUEST_TIMEOUT_SECONDS`
  - per-request timeout
  - default: `10`

## Exit Codes
- `0`: success
- `2`: invalid input or ambiguous selector
- `3`: not found
- `4`: conflict
- `10`: transport failure
- `11`: internal client or server error

## Current Limits
- `--profile`, `--refresh`, and `--offline` are parsed now so the command grammar is stable, but profile loading and catalog refresh behavior are not implemented yet.
- Call shorthand currently exposes Shiva call planning only. Direct-target execution, final request dispatch, and dry-run executor formatting are not shipped yet.
- Dynamic repo/API/operation/profile/target completions are not shipped yet.
- `ls`, `sync`, and `batch` are not shipped yet beyond their explicit command placeholders.

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
