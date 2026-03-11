# Shiva CLI Implementation Roadmap

Scope: Implement [`design/cli.md`](../design/cli.md) end-to-end by replacing the current draft read-only CLI and legacy path-shaped read transport with the query-driven CLI/runtime contract described in the design.

Documentation: [`design/cli.md`](../design/cli.md), [`docs/cli.md`](../docs/cli.md), [`docs/endpoints.md`](../docs/endpoints.md), [`docs/database.md`](../docs/database.md), [`docs/setup.md`](../docs/setup.md), [`docs/testing.md`](../docs/testing.md)

Legend: [ ] todo, [x] done.

- [x] 1.1 Lock the current CLI and transport gap set
  - Repository: `shiva`
  - Component: `cmd/shiva`, `internal/cli`, `internal/http`, `internal/store`, `docs`
  - Reasoning: `medium`
  - Verification: current CLI still accepts only repo and `#operationId`, current server still exposes `/v1/specs|routes`, current docs still describe the draft CLI limits
1. Read [`docs/cli.md`](../docs/cli.md), `internal/cli/root.go`, `internal/cli/selector.go`, `internal/cli/service.go`, `internal/http/server.go`, and `internal/http/read_routes.go` and record the exact shipped baseline.
2. Confirm `ParseSelector` and `NewRootCommand` only support `<repo-path>` and `<repo-path>#<operationId>`.
3. Confirm `DraftService` still resolves operations by downloading the full spec and scanning `operation_id` client-side.
4. Lock the missing scope to the design items that are not shipped yet: query transport, explicit revision/api selectors, catalog cache, targets, calls, batch, and dynamic completion.

- [x] 1.2 Add store and query primitives for CLI snapshot reads
  - Repository: `shiva`
  - Component: `sql/query`, `internal/store/sqlc`, `internal/store`
  - Reasoning: `high`
  - Verification: repo/API/operation inventories load by snapshot, `operation_id` lookup works within one API and across one repo snapshot, selector resolution supports default branch, short SHA, and revision id
1. Add SQL and store methods that resolve specs and operations by `repo`, optional `api`, and exactly one of `revision_id`, `sha`, or default-branch latest.
2. Add inventory queries for repos, APIs, and operations that return the fields required by the CLI catalog and `ls` output.
3. Add `operation_id` lookup paths scoped to one API snapshot and to one repo snapshot, and return candidate rows for ambiguity reporting instead of collapsing them too early.
4. Add catalog-freshness store data keyed by repo default-branch latest state so the CLI can refresh inventories without downloading full specs on every run.

- [x] 1.3 Replace legacy read routes with the query transport contract
  - Repository: `shiva`
  - Component: `internal/http`, `internal/store`
  - Reasoning: `xhigh`
  - Verification: `/v1/spec`, `/v1/operation`, `/v1/apis`, `/v1/operations`, `/v1/repos`, and `/v1/catalog/status` work end-to-end, invalid query combinations fail cleanly, legacy `/v1/specs|routes` read routes are removed
1. Split `internal/http/read_routes.go` into smaller handler files before expanding the read surface so the new transport does not pile onto one 500+ LOC file.
2. Register `GET /v1/spec`, `GET /v1/operation`, `GET /v1/apis`, `GET /v1/operations`, `GET /v1/repos`, and `GET /v1/catalog/status` in `internal/http/server.go`.
3. Add one shared query-normalization layer that validates `repo`, optional `api`, exclusive `revision_id|sha`, and `operation_id` vs `method+path` rules before calling the store.
4. Remove the legacy path-segment read handlers and compatibility fallbacks once the query handlers use the new API-scoped store primitives end-to-end.

- [x] 1.4 Add a shared request envelope and Shiva call transport
  - Repository: `shiva`
  - Component: `internal/http`, `internal/cli/request`, `internal/cli/executor`
  - Reasoning: `high`
  - Verification: `POST /v1/call` accepts normalized call envelopes, operation-id and method/path calls normalize to the same request plan, dry-run and batch reuse the same envelope shape
1. Define one normalized request envelope for inspect and call flows with explicit repo, api, revision, target, and request-input fields.
2. Add `POST /v1/call` to the HTTP server and route it through the same selector and operation-resolution rules used by the new read endpoints.
3. Keep executor-specific transport details outside the envelope so direct calls and Shiva-mediated calls share one normalization model and diverge only at dispatch time.
4. Add handler and service tests that cover envelope validation, ambiguous resolution, and no-network dry-run planning.

- [ ] 1.5 Rebuild the CLI command grammar around the designed shorthand
  - Repository: `shiva`
  - Component: `cmd/shiva`, `internal/cli`
  - Reasoning: `high`
  - Verification: root shorthand accepts repo, repo-plus-operation, method/path, and target forms; `ls`, `sync`, `batch`, `completion`, and `health` are explicit subcommands; invalid selector combinations fail with the documented exit codes
1. Replace the draft selector parser with one parser that understands packed `repo@target#operation`, explicit `<method> <cli-path>`, and `:param` path normalization.
2. Refactor `NewRootCommand` into a static Cobra tree with root shorthand dispatch plus explicit `ls`, `sync`, `batch`, `completion`, and `health` subcommands.
3. Add shared flag parsing and validation for `--api`, `--sha`, `--rev`, `--profile`, `--via`, `--refresh`, `--offline`, `--dry-run`, and output mode selection.
4. Keep exit-code mapping centralized so shorthand, utility subcommands, and completion all return the design contract of `0`, `2`, `3`, `4`, `10`, or `11`.

- [ ] 1.6 Add XDG-backed profiles, targets, and catalog cache
  - Repository: `shiva`
  - Component: `internal/cli/profile`, `internal/cli/target`, `internal/cli/catalog`, `internal/cli/httpclient`, `internal/cli/config`
  - Reasoning: `xhigh`
  - Verification: active profile and per-target source overrides load from config, default-branch snapshots refresh lazily, pinned SHA and revision snapshots reuse immutable cache entries, `--offline` runs from cache only
1. Replace the env-only draft CLI configuration with XDG-backed profile and target loaders while keeping only the minimal env overrides that the shipped CLI still needs.
2. Implement a catalog store keyed by source profile, repo, api, and selector scope under the user cache directory.
3. Refresh catalog slices through `/v1/catalog/status`, `/v1/repos`, `/v1/apis`, and `/v1/operations` before fetching full specs or executing calls.
4. Enforce refresh behavior in one catalog service so floating snapshots auto-refresh when stale, `--refresh` forces network, and `--offline` forbids network.

- [ ] 1.7 Implement inspect, list, and sync flows on top of the catalog
  - Repository: `shiva`
  - Component: `internal/cli/service`, `internal/cli/output`, `internal/cli/catalog`, `cmd/shiva`
  - Reasoning: `high`
  - Verification: full spec fetch, operation fetch, method/path lookup, `ls repos`, `ls apis`, `ls ops`, and `sync` all work for single-API and multi-API repos
1. Move operation resolution from client-side full-spec scanning to server-backed operation queries plus catalog-backed API selection.
2. Implement inspect flows for `<repo-ref>`, `<repo-ref>#<operationId>`, and `<repo-ref> <method> <cli-path>` with deterministic output defaults and explicit `-o` handling.
3. Implement `ls repos`, `ls apis`, `ls ops`, and `sync` against the new inventory endpoints and catalog service.
4. Make multi-API ambiguity and duplicate-operation errors print candidate data from the query transport instead of ad hoc client guesses.

- [ ] 1.8 Implement call execution, dry-run, and NDJSON batch flows
  - Repository: `shiva`
  - Component: `internal/cli/request`, `internal/cli/executor`, `internal/cli/output`, `cmd/shiva`
  - Reasoning: `xhigh`
  - Verification: direct target calls, `@shiva` calls, `--dry-run`, `ls --emit request`, and `batch` all share one request model and behave correctly on grouped refreshes
1. Build request assembly from resolved operation metadata plus repeated `--path`, `--query`, `--header`, `--json`, and `--body` inputs.
2. Implement direct-target and Shiva-target executors behind one dispatch layer that consumes the normalized request envelope.
3. Implement dry-run JSON and curl outputs without sending the final request while still running normal selector resolution and catalog policy.
4. Implement `batch` stdin and file NDJSON ingestion, grouped refresh reuse, and request-envelope compatibility with `ls --emit request`.

- [ ] 1.9 Add dynamic completion on top of static Cobra scripts
  - Repository: `shiva`
  - Component: `internal/cli/completion`, `internal/cli/catalog`, `cmd/shiva`
  - Reasoning: `medium`
  - Verification: repo, API, operation, profile, target, and method completions come from cache, stale cache refresh is best-effort, network failure falls back to cached values, completion does not block on long-running requests
1. Keep `shiva completion <shell>` as the single generated script entrypoint.
2. Add `ValidArgsFunction` and `RegisterFlagCompletionFunc` handlers for repo refs, packed selectors, `--api`, `--profile`, `--via`, and HTTP-method position completion.
3. Resolve completion values from the catalog cache first and use short-timeout refresh only for stale slices.
4. Keep completion failure-open so cached values still work when freshness checks or network reads fail.

- [ ] 1.10 Update long-lived docs and verification to match the shipped CLI
  - Repository: `shiva`
  - Component: `docs`, `internal/http`, `internal/store`, `internal/cli`, `cmd/shiva`
  - Reasoning: `medium`
  - Verification: focused package tests pass, end-to-end CLI contract scenarios pass, docs link checks pass, `docs/cli.md` describes the shipped CLI instead of the draft
1. Rewrite [`docs/cli.md`](../docs/cli.md), [`docs/endpoints.md`](../docs/endpoints.md), [`docs/database.md`](../docs/database.md), [`docs/setup.md`](../docs/setup.md), and [`docs/testing.md`](../docs/testing.md) to describe the shipped query transport, cache behavior, and call modes.
2. Add focused tests for query handlers, selector parsing, catalog refresh, list output, batch execution, dry-run behavior, and completion behavior.
3. Run focused packages first, then `go test ./...`, and add one end-to-end CLI contract suite that covers the milestone scenarios from [`design/cli.md`](../design/cli.md).
4. Remove the completed transient CLI design and roadmap docs once the implementation is fully shipped and `docs/` is self-sufficient.
