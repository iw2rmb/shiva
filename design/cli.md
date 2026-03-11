# Shiva CLI Design

## Summary
This design defines a short, script-first Shiva CLI for fetching specs, inspecting operations, and executing requests derived from stored specs. The hot path is a root-dispatched command surface:

- `shiva <repo-ref>`
- `shiva <repo-ref>#<operationId>`
- `shiva <repo-ref> <method> <path>`
- `shiva <repo-ref>@<target>#<operationId>`
- `shiva <repo-ref>@<target> <method> <path>`

The design uses `path_with_namespace` as the repo identity, prefers `operation_id` for concise operation lookup, defaults reads to the repo default branch, and adds source-profile selection, execution targets, `--dry-run`, pipe-safe NDJSON, shell completions, and a refreshable local catalog used for operation resolution and completion values.

The implementation uses Cobra for parsing, help, aliases, and shell completion generation, but the user-facing contract is not a deep command tree.

## Scope
In scope:
- New `cmd/shiva` CLI binary.
- Root command grammar for spec fetch, operation fetch, and operation execution.
- Short repo, revision, operation, path, and target selector notation.
- File-based source-profile selection for Shiva environments.
- File-based execution-target selection for direct and Shiva-mediated calls.
- Refreshable local catalog/cache for repos, APIs, operations, and operation metadata.
- `--dry-run` behavior.
- Pipe-oriented listing and batch execution behavior.
- Shell completion behavior.
- Backend read API and store/query changes required to support the CLI contract.

Out of scope:
- Generic remote shell execution over SSH, `kubectl exec`, or similar host transports.
- CLI write/mutation commands in v1.
- Interactive TUI output.
- Preserving backward compatibility with current path-segment read routes.
- Branch-name selectors in the CLI surface.
- CLI config mutation subcommands; profiles are file-based in v1.

## Why This Is Needed
- The repository currently ships only the server binary in [`cmd/shivad/main.go`](../cmd/shivad/main.go). There is no user CLI.
- Current read APIs are HTTP-route-oriented and path-segment-based in [`internal/http/server.go`](../internal/http/server.go), which is still too verbose for tests and shell usage.
- Repo identity in storage is GitLab `path_with_namespace`, not a single path segment, as documented in [`docs/database.md`](../docs/database.md). A CLI and transport contract must support repo names like `allure/allure-deployment`.
- OpenAPI paths use `{id}` placeholders. In shells, especially `zsh`, that forces quoting or brace escaping. A CLI-specific path notation is needed.
- `operation_id` is already extracted and stored alongside `(method, path)` in endpoint indexing, as described in [`docs/endpoints.md`](../docs/endpoints.md) and implemented in [`internal/openapi/canonical.go`](../internal/openapi/canonical.go).
- The current read API still requires URL-escaped repo path segments in routes such as `/v1/specs/{repo}/...` and `/v1/routes/{repo}/...`, which is not the desired transport contract for a script-first CLI.

## Goals
- Make the hot path short enough for tests and frequent shell use.
- Keep one stable repo reference grammar.
- Keep the CLI grammar static while allowing runtime data to refresh dynamically.
- Avoid `-X METHOD` and avoid braces in the primary path notation.
- Support both `operationId` lookup and `(method, path)` lookup.
- Use the same selector grammar for inspect, dry-run, direct calls, and Shiva-mediated calls.
- Default reads to the repo default branch and latest processed OpenAPI state.
- Keep API selection optional only when resolution is unambiguous.
- Make command output pipe-safe and machine-readable.
- Support named source profiles and named execution targets.
- Provide fast shell completions without making interactive usage dependent on network round-trips.

## Non-goals
- Reusing the current path-segment read-route shape and only wrapping it with a CLI.
- Making `operation_id` the canonical storage key.
- Supporting ambiguous implicit API selection.
- Supporting arbitrary branch names in CLI selectors.
- Rebuilding the Cobra command tree from remote specs at runtime.
- Regenerating shell completion scripts before every command execution.
- Optimizing for manual browser usage of the read API over CLI/script usage.

## Current Baseline (Observed)
- The only command binary today is `shivad` in [`cmd/shivad/main.go`](../cmd/shivad/main.go).
- The current HTTP server registers read routes under `/v1/specs/:repo` and `/v1/routes/:repo` in [`internal/http/server.go`](../internal/http/server.go).
- Read resolution resolves repos directly by `path_with_namespace` in [`internal/store/read_selector.go`](../internal/store/read_selector.go).
- No-selector reads resolve against `repos.default_branch` in [`internal/store/read_selector.go`](../internal/store/read_selector.go).
- Endpoint extraction stores `method`, `path`, `operation_id`, `summary`, `deprecated`, and `raw_json`, and the persisted unique endpoint identity remains `(api_spec_revision_id, method, path)`, as documented in [`docs/endpoints.md`](../docs/endpoints.md).
- Multi-API repos already exist in the data model through `api_specs` and `api_spec_revisions`, as documented in [`docs/database.md`](../docs/database.md) and reflected in the current read-route contract in [`docs/endpoints.md`](../docs/endpoints.md).
- Observed local DB sample on 2026-03-11:
  - repo `allure/allure-deployment`
  - API root `service-catalog/allure-api.yaml`
  - `revision_id=146`
  - 551 of 551 indexed operations on that API revision had non-empty `operation_id`
  - multi-API repos also exist, for example `tip/tip-invoice` with 4 API roots

## Target Contract or Target Architecture

### Command Model
- Use Cobra as the CLI framework.
- Keep root-level inspect and call forms implicit and short.
- Keep the command tree static. Runtime data is dynamic, not the command structure.
- Reserve explicit subcommands for non-fetch utility flows:
  - `shiva ls ...`
  - `shiva sync ...`
  - `shiva batch`
  - `shiva completion ...`
  - `shiva health`
- Do not require `shiva spec get`, `shiva op show`, or `shiva call` for the normal read path.

### Static Grammar, Dynamic Catalog
The CLI syntax is static. The repo/API/operation universe is dynamic.

Rules:
- The Cobra command tree is declared once at startup and does not change based on fetched specs.
- Generated shell completion scripts are static and are installed once by the user.
- The CLI maintains a local catalog cache for:
  - repos
  - API roots
  - operation inventory
  - operation metadata needed for request assembly
- Command execution may refresh the local catalog before resolution when the relevant snapshot is stale.
- Completion functions may refresh the local catalog opportunistically, but completion must continue to work from cached data if refresh fails or times out.
- Dynamic behavior is expressed through cached values and runtime resolution, not through dynamic command registration.

### Root Grammar
The root command accepts five primary forms:

```text
shiva [global-flags] <repo-ref>
shiva [global-flags] <repo-ref>#<operation-id>
shiva [global-flags] <repo-ref> <method> <cli-path>
shiva [global-flags] <repo-ref>@<target>#<operation-id> [call-input-flags]
shiva [global-flags] <repo-ref>@<target> <method> <cli-path> [call-input-flags]
```

Examples:

```bash
shiva allure/allure-deployment
shiva allure/allure-deployment#findAll_42
shiva allure/allure-deployment post /accessgroup/:id/user
shiva allure/allure-deployment@prod#getUsers --path id=42
shiva allure/allure-deployment@shiva post /accessgroup/:id/user --path id=42 --json @payload.json
```

### Packed Selector Grammar
The packed selector uses repo path plus optional call target and optional `operationId`:

```text
<repo-path>
<repo-path>#<operation-id>
<repo-path>@<target>#<operation-id>
```

Rules:
- `<repo-path>` is the raw GitLab `path_with_namespace`, for example `allure/allure-deployment`.
- `@<target>` selects an execution target and is equivalent to `--via <target>`.
- `#<operation-id>` selects `operationId`.
- `#<operation-id>` must be part of the same shell token as the repo selector. A standalone `#...` argument is invalid because shells treat it as a comment start.
- `<repo-path>@<target>` without `#<operation-id>` is valid only for the `<method> <path>` call form.
- Revision selection is explicit and uses `--sha <sha8>` or `--rev <revision-id>`.
- If both `@<target>` and `--via <target>` are present, they must match.

Examples:

```bash
shiva allure/allure-deployment#findAll_42
shiva allure/allure-deployment@prod#getUsers
shiva allure/allure-deployment@shiva#getUsers --path id=42
shiva allure/allure-deployment@prod post /accessgroup/:id/user
```

### Path Grammar
`cli-path` is a CLI-specific route notation, not raw OpenAPI syntax.

Rules:
- The path must start with `/`.
- Dynamic segments use `:name`, not `{name}`.
- Before lookup, the CLI normalizes path segments of form `:name` into `{name}`.
- Literal `{name}` remains accepted as a fallback input form, but `:name` is the documented primary notation.
- The CLI does not infer the method from the path.

Examples:

```bash
shiva allure/allure-deployment get /accessgroup
shiva allure/allure-deployment#findAll_42
shiva allure/allure-deployment@prod post /accessgroup/:id/user
shiva allure/allure-deployment@prod patch /cf/:id
```

This avoids shell brace expansion and removes the need for quoting in normal OpenAPI path lookups.

### Operation Selection
Operation lookup supports two forms:

1. `#operationId`
2. `<method> <path>`

Rules:
- `#operationId` does not accept a method prefix.
- `operationId` lookup is exact-match and case-sensitive.
- Method lookup accepts any case from the user and normalizes to lowercase before querying.
- Method/path lookup uses the canonical persisted key `(method, path)`.
- `operationId` is a convenience selector, not the canonical storage key.

Examples from the observed local sample:

```bash
shiva allure/allure-deployment#findAll_42
shiva allure/allure-deployment#patchById
shiva allure/allure-deployment get /accessgroup
shiva allure/allure-deployment@prod#getUsers --path id=42
shiva allure/allure-deployment post /accessgroup/:id/user
```

### API Selection Rules
API root selection uses `-a, --api <root-path>` when needed.

Rules:
- If the selected repo snapshot contains exactly one API root, `--api` is optional.
- If the selected repo snapshot contains multiple API roots:
  - full spec fetch without `--api` is an error,
  - operation fetch without `--api` attempts unique cross-API resolution,
  - if exactly one API root matches the selector, resolve to it,
  - if zero or multiple API roots match, return an ambiguity error and list candidate API roots.
- `--api` always wins over cross-API inference.

Examples:

```bash
shiva tip/tip-invoice --api api/docs/src/main/resources/InvoiceApi.yaml
shiva tip/tip-invoice#createInvoice --api api/docs/src/main/resources/InvoiceApi.yaml
```

### Default Revision Rules
If the command does not include `--sha` or `--rev`, the read target is:

- the latest processed OpenAPI state
- on the repo's stored `default_branch`

Rules:
- The backend must stop hardcoding `main`.
- If the latest revision row on the repo default branch exists but is not yet processed, the command returns a conflict (`409` on HTTP transport, non-zero CLI exit).
- If the repo has no processed OpenAPI state on the default branch, the command returns not found.

This keeps the CLI aligned with repo metadata rather than a global branch assumption.

### Catalog Cache and Refresh Rules
The CLI resolves selectors against a local refreshable catalog.

Cache key dimensions:
- `source_profile`
- `repo`
- `api`
- selector scope:
  - `default-branch-latest`
  - `sha:<sha8>`
  - `rev:<revision-id>`

Catalog payloads:
- repo identity and metadata
- API root inventory
- operation inventory
- operation request metadata required for call assembly

Rules:
- Floating reads without `--sha` or `--rev` use the `default-branch-latest` cache key.
- Pinned reads with `--sha` or `--rev` use immutable cache keys and may be cached indefinitely.
- Before inspect or call execution, the CLI checks freshness of the relevant catalog slice.
- If the relevant catalog slice is stale, the CLI refreshes it before operation resolution.
- Full spec bodies are not the default refresh unit. Inventory and operation metadata are fetched first; full spec fetch remains explicit.
- Refresh failure falls back to cached data only when that data is present and the command is not marked `--refresh`.
- `--refresh` forces a catalog refresh before execution.
- `--offline` forbids network refresh and uses cached data only.
- `shiva sync <repo-ref>` explicitly refreshes catalog state without executing a read or call.

Examples:

```bash
shiva sync allure/allure-deployment
shiva allure/allure-deployment#findAll_42 --refresh
shiva allure/allure-deployment@prod#getUsers --offline --path id=42
```

### Call Modes and Targets
Calls reuse the same selector grammar as inspect commands.

Rules:
- No `@<target>` and no `--via <target>` means inspect-only behavior.
- `@<target>` or `--via <target>` means call mode.
- Call mode requires either `#<operationId>` or `<method> <path>`.
- Targets are named execution backends, not raw URLs on the command line.
- A target with mode `direct` sends the resolved request to the configured host/base URL.
- A target with mode `shiva` sends the normalized request to Shiva's call endpoint for validation/mock execution.
- `@shiva` is equivalent to `--via shiva`.
- `--dry-run` prints the resolved request plus the chosen executor and does not send the call to the target.
- `--dry-run` may still resolve the operation from Shiva before printing the call plan.
- Call resolution uses the same catalog refresh policy as inspect resolution.

Examples:

```bash
shiva allure/allure-deployment#findAll_42
shiva allure/allure-deployment@prod#getUsers --path id=42
shiva allure/allure-deployment@shiva#getUsers --path id=42
shiva allure/allure-deployment@prod post /accessgroup/:id/user --path id=42 --json @payload.json
shiva allure/allure-deployment@prod#getUsers --path id=42 --dry-run
shiva allure/allure-deployment#getUsers --via shiva --path id=42 --dry-run
```

### Call Input Flags
Operation calls are parameterized by repeated flat flags.

Flags:
- `--path key=value`
- `--query key=value`
- `--header Name=value`
- `--json <inline-json|@file>`
- `--body @file`

Rules:
- `--path` populates path parameters before URL assembly.
- `--query` may be repeated.
- `--header` may be repeated.
- `--json` and `--body` are mutually exclusive.
- The CLI is responsible for path interpolation and final request assembly after operation resolution.

### Output Contract
Output is command-type specific and pipe-safe.

Rules:
- Command results go to stdout.
- Logs, progress, and human-readable errors go to stderr.
- `-o, --output` is explicit and deterministic.
- Supported output modes:
  - spec fetch: `yaml`, `json`
  - operation fetch: `json`, `yaml`
  - call: `body`, `json`
  - dry-run: `json`, `curl`
  - list commands: `table`, `tsv`, `json`, `ndjson`
  - batch: `json`, `ndjson`
- `table` is never the only format for a list command.

Default output modes:
- `shiva <repo-ref>` => `yaml`
- `shiva <repo-ref>#<operationId>` => `json`
- `shiva <repo-ref> <method> <path>` => `json`
- `shiva <repo-ref>@<target>#<operationId>` => `body`
- `shiva <repo-ref>@<target> <method> <path>` => `body`
- `shiva ls ...` => `table` on TTY, `ndjson` otherwise

### Listing Commands
Listing is explicit and short:

```text
shiva ls repos
shiva ls apis <repo-ref>
shiva ls ops <repo-ref> [--api <root-path>]
shiva sync <repo-ref>
```

Examples:

```bash
shiva ls repos
shiva ls apis tip/tip-invoice
shiva ls ops allure/allure-deployment
shiva ls ops tip/tip-invoice --api api/docs/src/main/resources/InvoiceApi.yaml
shiva sync allure/allure-deployment
```

`ls ops` rows include enough metadata for scripting:
- `repo`
- `api`
- `method`
- `path`
- `operation_id`
- `summary`
- `deprecated`

### Pipe Contract
The CLI must support command-to-command pipelines without shell-specific parsing glue.

Rules:
- `shiva ls ... -o ndjson` emits row objects.
- `shiva ls ... --emit request` emits executable request envelopes as NDJSON.
- `shiva batch` reads request envelopes from stdin or `--from <file>`.
- `shiva batch` executes the same request model as the root shorthand.
- `--dry-run` works on both root shorthand and `batch`.
- `batch` may refresh catalogs per request or per grouped repo snapshot, but it should coalesce refreshes for identical cache keys.

Request envelope examples:

```json
{"kind":"spec","repo":"allure/allure-deployment","api":"service-catalog/allure-api.yaml","revision_id":146}
{"kind":"operation","repo":"allure/allure-deployment","api":"service-catalog/allure-api.yaml","revision_id":146,"operation_id":"findAll_42"}
{"kind":"call","repo":"allure/allure-deployment","api":"service-catalog/allure-api.yaml","revision_id":146,"target":"prod","operation_id":"getUsers","path_params":{"id":"42"}}
{"kind":"call","repo":"allure/allure-deployment","api":"service-catalog/allure-api.yaml","revision_id":146,"target":"shiva","method":"post","path":"/accessgroup/{id}/user","path_params":{"id":"42"}}
```

Pipeline examples:

```bash
shiva ls ops allure/allure-deployment --emit request | shiva batch
shiva ls ops allure/allure-deployment -o ndjson | jq -r '.operation_id'
```

### Profiles and Targets
Source resolution and execution target selection are separate concerns.

Rules:
- Source profiles are read from an XDG config file, for example `~/.config/shiva/profiles.yaml`.
- A top-level `active_profile` defines the default Shiva source for inspect commands and for call resolution when the target does not override it.
- `--profile <name>` overrides the active source profile.
- Execution targets are read from the same config surface or a neighboring target file.
- `@<target>` and `--via <target>` select the same execution target.
- A target may declare its own `source_profile` to resolve operations against a different Shiva environment than the active profile.

Minimum source-profile fields:
- `base_url`
- `token` or auth source reference
- `timeout`

Minimum target fields:
- `mode`: `direct` or `shiva`
- `source_profile` (optional)
- `base_url`
- `token` or auth source reference (optional)
- `timeout`

Examples:

```bash
shiva --profile local allure/allure-deployment#findAll_42
shiva allure/allure-deployment@prod#getUsers
shiva allure/allure-deployment#getUsers --via shiva
```

### Dry Run
`--dry-run` prints the normalized request plan and the outgoing executor request without sending it.

Rules:
- No request is sent to the final call target in dry-run mode.
- Exit code is `0` if normalization succeeds.
- Dry-run output goes to stdout and is machine-readable JSON by default.
- `-o curl` is allowed for direct-call dry-runs.
- Dry-run still allows catalog refresh and operation resolution unless `--offline` is also set.

Example:

```bash
shiva allure/allure-deployment@prod#getUsers --path id=42 --dry-run
```

### Completion Contract
Shell completion is built on Cobra's generated completion scripts plus dynamic value completion.

Rules:
- `shiva completion bash|zsh|fish|powershell` is always available.
- Dynamic completions are provided for:
  - profile names
  - target names after `@` in the first token or after `--via`
  - repo refs
  - API roots after `--api`
  - operation IDs after `#` in the first token
  - HTTP methods in the method position
- Dynamic completions use the local catalog cache.
- Completion may perform a short best-effort refresh when the relevant cache slice is stale.
- Completion must fail open to cached values on network errors or refresh timeout.
- Completion lookups must never block the shell for long-running network work.
- Completion scripts are not regenerated automatically by runtime refresh.

### Exit Codes
- `0`: success
- `2`: invalid CLI input or ambiguous selector
- `3`: not found
- `4`: conflict or unresolved processing state
- `10`: auth or transport failure
- `11`: internal client or server error

### HTTP Transport Contract
The CLI request model is transported over explicit query-driven HTTP endpoints instead of path-heavy REST routes.

Endpoints:
- `GET /v1/spec`
- `GET /v1/operation`
- `POST /v1/call`
- `GET /v1/apis`
- `GET /v1/operations`
- `GET /v1/repos`
- `GET /v1/catalog/status`

Rules:
- All resource identity parameters are query parameters, not URL path segments.
- `repo` uses raw `path_with_namespace`.
- `api` uses raw `root_path`.
- Exactly one of `revision_id`, `sha`, or neither is allowed.
- `neither` means default-branch latest processed OpenAPI resolution.
- `operation_id` is mutually exclusive with `method` and `path`.
- `POST /v1/call` accepts the normalized request envelope used by CLI call mode.
- `POST /v1/call` is the transport used by targets with mode `shiva`.
- `GET /v1/catalog/status` returns freshness metadata for default-branch snapshots so the CLI can avoid refetching unchanged catalogs.

Examples:

```text
GET /v1/spec?repo=allure%2Fallure-deployment
GET /v1/spec?repo=tip%2Ftip-invoice&api=api%2Fdocs%2Fsrc%2Fmain%2Fresources%2FInvoiceApi.yaml&revision_id=40&format=yaml
GET /v1/operation?repo=allure%2Fallure-deployment&operation_id=findAll_42
GET /v1/operation?repo=allure%2Fallure-deployment&method=post&path=%2Faccessgroup%2F%7Bid%7D%2Fuser
GET /v1/catalog/status?repo=allure%2Fallure-deployment
POST /v1/call {"repo":"allure/allure-deployment","operation_id":"getUsers","path_params":{"id":"42"}}
```

This transport contract avoids the current ambiguity of path-segment routing once repo identity is `path_with_namespace`.

## Implementation Notes

### CLI Structure
- Add `cmd/shiva`.
- Add a dedicated CLI package tree, for example:
  - `internal/cli/parse`
  - `internal/cli/profile`
  - `internal/cli/target`
  - `internal/cli/catalog`
  - `internal/cli/request`
  - `internal/cli/executor`
  - `internal/cli/httpclient`
  - `internal/cli/output`
  - `internal/cli/completion`
- The Cobra root command owns shorthand parsing after normal subcommands fail to match.
- The shorthand parser must understand packed selectors of form `repo#op` and `repo@target#op`.

### Cobra Fit
Cobra is suitable for this design if Shiva keeps the command grammar static.

Why it fits:
- Cobra handles the stable command tree, flags, help, aliases, and completion script generation.
- Cobra supports dynamic positional completion through `ValidArgsFunction`.
- Cobra supports dynamic flag completion through `RegisterFlagCompletionFunc`.
- Cobra exposes command traversal and normal `RunE` execution hooks, which are enough for root-level shorthand parsing and cache refresh before execution.

Implementation approach:
- Root command:
  - utility subcommands are registered normally with `AddCommand`
  - the root command itself has `RunE` for shorthand dispatch
  - root `Args` accepts the positional forms described in this design
- Before `RunE` executes a shorthand request:
  - load profile and target config
  - load catalog cache
  - refresh stale slices when needed
  - resolve repo/API/operation
  - dispatch to inspect, direct-call, or Shiva-call execution
- Completion:
  - install one generated completion script with `shiva completion ...`
  - use `ValidArgsFunction` on commands with positional dynamic values
  - use `RegisterFlagCompletionFunc` for flags such as `--api`, `--profile`, and `--via`
  - completion functions read from cache first and may do a short refresh

Unsuitable shape:
- Dynamically creating a new Cobra subcommand per repo, API, or operation at runtime
- Regenerating completion scripts before each command

If Shiva wanted truly dynamic command names such as `shiva repo opName ...` with remote-driven command registration, Cobra would become the wrong abstraction. That is not the recommended design here.

### Read Resolution Changes
- Add direct lookup by `revision_id`.
- Add direct lookup by short SHA from explicit `--sha`.
- Add store queries for:
  - list API roots by repo snapshot,
  - resolve operation by `operation_id` within one API revision,
  - resolve operation by `operation_id` across all API revisions in one repo snapshot,
  - resolve operation by `(method, path)` across all API revisions in one repo snapshot.
- Add an index for `operation_id` lookups, scoped by `api_spec_revision_id`.

### HTTP Server Changes
- Replace current path-segment read endpoints with the query-based endpoints defined above.
- Keep internal webhook and health routes as separate concerns.
- Make read endpoint handlers return simpler CLI-friendly payloads:
  - full spec body for `/v1/spec`,
  - raw operation object for `/v1/operation`,
  - list rows for `/v1/apis`, `/v1/operations`, `/v1/repos`.
- Add `POST /v1/call` that accepts the normalized call envelope and routes it through Shiva-side execution.
- `POST /v1/call` is the future attachment point for request schema validation and mock execution behavior.
- Add catalog freshness support so the CLI can cheaply detect whether cached default-branch operation inventories are still current.

### Output and Batch
- Implement an internal request envelope type shared by root shorthand, `ls --emit request`, and `batch`.
- Keep batch execution streaming and NDJSON-friendly.
- Keep stdout clean enough for `jq`, `awk`, and test assertions.
- Keep one shared request/envelope model across inspect, dry-run, direct call, and Shiva call paths.

### Catalog Client
- Implement a local catalog store keyed by profile, repo, API, and selector scope.
- Persist cached catalogs under the user's XDG cache directory.
- Track freshness with revision fingerprints or ETag-like server metadata.
- Refresh inventory endpoints before fetching full spec bodies whenever possible.
- Keep refresh timeouts short for shell completion paths.

### Documentation Updates
Implementation must update the long-lived docs after the code lands:
- [`docs/endpoints.md`](../docs/endpoints.md)
- [`docs/database.md`](../docs/database.md)
- [`docs/setup.md`](../docs/setup.md)
- [`docs/testing.md`](../docs/testing.md)
- [`docs/webhooks.md`](../docs/webhooks.md)

## Milestones

### Milestone 1: Add Query-Based Read Endpoints
Scope:
- Replace path-segment read routes with `/v1/spec`, `/v1/operation`, `/v1/call`, `/v1/apis`, `/v1/operations`, and `/v1/repos`.
- Add operation lookup by `operation_id`.
- Implement ambiguity handling for multi-API repos.
- Add catalog freshness endpoint or equivalent metadata contract.

Expected Results:
- Repo paths and API root paths no longer require route delimiter tricks.
- `operation_id` becomes a first-class lookup input.
- Shiva-mediated execution has a dedicated endpoint.
- The CLI can cheaply decide whether a cached floating snapshot needs refresh.

Testable outcome:
- `GET /v1/operation?repo=...&operation_id=...` returns the operation object for unique matches.
- `POST /v1/call` accepts normalized call input and resolves the target operation.
- `GET /v1/catalog/status?...` or equivalent freshness metadata lets the CLI skip unnecessary catalog downloads.
- Multi-API ambiguity returns an error payload that lists candidates.

### Milestone 2: Ship `cmd/shiva`
Scope:
- Implement Cobra root parser, profile loading, catalog cache/refresh, dry-run, output formatting, and utility subcommands.
- Implement `ls`, `batch`, and `completion`.

Expected Results:
- Users can fetch specs and operations with the short forms in this document.
- The CLI can list objects, stream request envelopes, and execute them via `batch`.
- Command execution and completion stay current without dynamic command regeneration.

Testable outcome:
- `shiva allure/allure-deployment#findAll_42`
- `shiva allure/allure-deployment post /accessgroup/:id/user`
- `shiva allure/allure-deployment@prod#getUsers --path id=42`
- `shiva allure/allure-deployment#getUsers --via shiva --path id=42`
- `shiva allure/allure-deployment#findAll_42 --refresh`
- `shiva sync allure/allure-deployment`
- `shiva ls ops allure/allure-deployment --emit request | shiva batch`
- `shiva completion zsh`

## Acceptance Criteria
- The primary read path uses raw `path_with_namespace` in CLI and query parameters in HTTP.
- `shiva <repo-ref>` fetches the full spec for unambiguous API selection.
- `shiva <repo-ref>#<operationId>` fetches an operation without requiring a method.
- `shiva <repo-ref> <method> <path>` supports `:param` syntax and does not require `-X`.
- `shiva <repo-ref>@<target>#<operationId>` executes a request against the selected target.
- `@shiva` and `--via shiva` are equivalent target selectors.
- The command tree and completion scripts stay static across runtime refreshes.
- Execution and completion resolve against a refreshable local catalog.
- Omitting API selection works only when the target API is unambiguous.
- Omitting revision selection uses the repo default branch, not a hardcoded branch name.
- Revision selection uses explicit `--sha` or `--rev`, not packed syntax.
- `--dry-run` emits machine-readable request data and performs no network I/O.
- `ls` plus `batch` supports CLI-to-CLI NDJSON pipelines.
- Completion scripts generate successfully and dynamic completions fail open.
- Long-lived docs are updated to match the shipped behavior after implementation.

## Risks
- Some specs may have duplicate or missing `operation_id`; ambiguity handling must stay precise and visible.
- Cross-API resolution without `--api` can surprise users if identical selectors appear in multiple APIs.
- Query endpoint migration replaces the current read API surface and will require broad test rewrites.
- Packed selector parsing must be careful about `#` shell-comment behavior and `@target` parsing.
- Over-eager refresh before every command can make the CLI feel slow if freshness checks are not cheap.
- Dynamic completion can become stale or noisy if cache invalidation is weak.
- Dynamic completion can become slow or noisy if cache behavior is poor.
- Batch/request envelope formats can ossify if shipped without a small, explicit contract.

## References
- Current read-route contract: [`docs/endpoints.md`](../docs/endpoints.md)
- Current schema and entities: [`docs/database.md`](../docs/database.md)
- Runtime config and startup behavior: [`docs/setup.md`](../docs/setup.md)
- Monorepo API identity and read-route contract: [`docs/endpoints.md`](../docs/endpoints.md)
- Current server route registration: [`internal/http/server.go`](../internal/http/server.go)
- Current read selector implementation: [`internal/store/read_selector.go`](../internal/store/read_selector.go)
- Current config defaults: [`internal/config/config.go`](../internal/config/config.go)
- Current OpenAPI endpoint extraction: [`internal/openapi/canonical.go`](../internal/openapi/canonical.go)
