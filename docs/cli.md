# CLI

## Scope
This document describes the shipped `shiva` CLI surface, selector grammar, runtime query behavior, and inspect/call execution modes.

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
  - `shiva ls <namespace-prefix>`
  - `shiva ls <namespace>/`
  - `shiva ls <namespace>/<repo-prefix>`
  - `shiva ls <namespace>/<repo>`
  - `shiva sync <repo-ref>`
  - `shiva tui`
  - `shiva tui <namespace>/`
  - `shiva tui <namespace>/<repo>`
  - `shiva batch`

`completion`, `health`, `ls`, `sync`, `tui`, and `batch` are all implemented.

## Selector Grammar
- `repo-ref` keeps the `<namespace>/<repo>` shorthand, for example `allure/allure-deployment`.
- Structured request envelopes split that identity into `namespace` and `repo`.
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
- `ls` accepts only selector input plus `--profile` and `--offline`.
- `tui` accepts only `--profile` and `--offline`.
- `tui` selector input is limited to:
  - no selector
  - `<namespace>/`
  - `<namespace>/<repo>`

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
- `@shiva` calls dispatch to `POST /v1/call`.
- Direct targets resolve the operation from `GET /v1/operations` and dispatch the final HTTP request from the CLI process.
- `shiva health` uses `GET /healthz`.
- The CLI no longer resolves `operationId` by downloading a full spec and scanning it client-side.
- Packed and positional shorthand are normalized into the shared request-envelope model before execution.
- Structured HTTP requests send separate `namespace` and `repo` fields.

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
- `shiva ls`
  - output is always plain text
  - `shiva ls`
    - prints namespaces that have at least one repo with an active spec as `<namespace>\t<repo-count> repos{, all pending}`
    - header: `total: <count>`
  - `shiva ls <namespace-prefix>`
    - prints matching namespaces
    - header: `match: <count>`
  - `shiva ls <namespace>/`
    - prints repos for that exact namespace that have at least one active spec
    - header: `namespace <namespace>, total <count> repos`
  - `shiva ls <namespace>/<repo-prefix>`
    - prints matching repos inside that namespace
    - header: `namespace <namespace>, match <count> repos`
  - `shiva ls <namespace>/<repo>`
    - prints one repo summary followed by repo-wide operations without preloading global repo inventory
    - repo summaries use `pending`, `processing`, or `<branch> (<sha8>), <ops>, updated DD-MM-YYYY HH:mm:ss`; when writing to a terminal, zero-op repo summaries are dimmed
    - repo-wide operations are sorted by path, grouped by top-level path segment with blank separator rows, right-align HTTP methods so paths start in one column, print bold `#<operation-id>` in an aligned second column, and display path params as `:name`; when writing to a terminal, params are bold, methods are colorized by verb, and summaries render dimmed
- `shiva sync <repo-ref>`
  - output: JSON summary with namespace, repo, cache scope, resolved snapshot revision when known, API count, and refreshed operation-catalog count
- `shiva batch`
  - reads request envelopes from stdin or `--from <file>`
  - default output: `ndjson`
  - supported `-o/--output`: `ndjson`, `json`
  - emits result rows with `index`, `request`, `ok`, `output`, and `error`
- `shiva tui`
  - starts a read-only terminal UI shell
  - initial route is selected by the optional argument:
    - no selector starts in the `SHIVA` home list
    - `<namespace>/` starts in that namespace's repo view
    - `<namespace>/<repo>` starts in that repo's explorer view
  - startup for home route preloads only namespace count from `/v1/namespaces/count` and shows it in the `Repos` item as `Total: <count>`
  - startup for home route does not preload namespace rows; namespace rows are loaded when entering namespace mode
  - startup for namespace route loads namespace catalog from `/v1/namespaces`
  - startup for direct namespace route (`shiva tui <namespace>/`) loads both namespace and repo catalogs (`/v1/namespaces` + `/v1/repos`) before interactions
  - startup for direct repo route (`shiva tui <namespace>/<repo>`) skips repo catalog preload and loads repo operations immediately
  - home mode:
    - list title: `SHIVA`
    - entries: `Repos`, `Endpoints`
    - `Repos` description includes `Total: <namespace-count>`
    - `enter` on `Repos` opens namespace mode
  - namespace mode:
    - built-in list filtering is enabled
    - `up` and `down` move the selection
    - `enter` opens the selected namespace's repo view
    - `esc` returns to home mode
  - repo mode:
    - `up` and `down` move the selection
    - `enter` opens the selected repo explorer
    - `esc` returns to namespace mode
  - repo explorer mode:
    - left pane lists repo endpoints (sorted deterministically by path, method, operation id, and API)
    - right pane renders markdown details in tabs: `Endpoints`, `Servers`, `Errors`
    - markdown style uses `GLAMOUR_STYLE` when set; otherwise defaults to `dark` (no terminal background probe)
    - `tab` and `shift+tab` switch detail tabs
    - `pgup`, `pgdown`, `home`, `end`, `ctrl+u`, and `ctrl+d` scroll the detail viewport
    - `esc` returns to repo mode
    - operation detail loads lazily for selected endpoint and is cached by endpoint identity
    - spec detail loads only for `Servers` when operation-level servers are missing/empty and is cached by `(namespace, repo, api)`
    - wide terminals render side-by-side panes; narrow terminals stack panes vertically
  - empty catalogs and startup catalog-load failures render explicit deterministic states
  - `q` and `ctrl+c` quit from any route

Success writes to stdout. Errors write to stderr.

## Request Envelopes
- `batch` accepts:
  - `{"kind":"spec","namespace":"<ns>","repo":"<slug>", ...}`
  - `{"kind":"operation","namespace":"<ns>","repo":"<slug>", ...}`
  - `{"kind":"call","namespace":"<ns>","repo":"<slug>", ...}`
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

## Local Catalog Cache
- Runtime CLI requests use live query endpoints (`/v1/spec`, `/v1/operation`, `/v1/apis`, `/v1/operations`, `/v1/namespaces`, `/v1/repos`) and do not use the local catalog cache.
- Local catalog cache remains for completion provider flows under `$XDG_CACHE_HOME/shiva/catalog/v1` or `~/.cache/shiva/catalog/v1`.
- `--offline` no longer provides cached inspect/call behavior; inspect and call flows require live connectivity.
- `shiva sync <repo-ref>` performs live refresh checks via `/v1/apis` and per-API `/v1/operations` and returns a JSON summary.

## Exit Codes
- `0`: success
- `2`: invalid input or ambiguous selector
- `3`: not found
- `4`: conflict
- `10`: transport failure
- `11`: internal client or server error

## Current Limits
- Dynamic repo/API/operation/profile/target and HTTP-method completions are shipped through the generated Cobra completion script.
- At the top-level prompt, generated bash/zsh completion prefers repo selectors over root subcommand names when repo candidates are available.
- `shiva ls <TAB>` uses the same repo-selector completion path; the old `ls repos|apis|ops` subcommands no longer exist.
- Repo selector completion walks namespace segments first, then completes repo leaves inside the selected namespace.
- Repo selector completion annotates namespace entries with repo counts and adds `all pending` when every repo under that namespace is still unprocessed.
- Final repo entries annotate `pending`/`processing` or `updated YYYY-MM-DD`; `ls` itself prints the fuller repo summary including branch, sha, ops, and timestamp.
- Completion reads from the local catalog cache first and may do a short best-effort refresh for stale slices before falling back to cached values.
- Namespace repo listings may trigger one repo-scoped API inventory lookup per non-pending repo in the selected namespace so they can show branch and operation-count metadata.

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
- Invoking the binary with a path prefix bypasses the generated `shiva` completion binding.

## References
- Endpoint transport contract: `docs/endpoints.md`
- Runtime configuration: `docs/setup.md`
- Test coverage and commands: `docs/testing.md`
- Storage and snapshot selectors: `docs/database.md`
