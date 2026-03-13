# TUI Architecture

## Summary
Shiva needs a terminal UI that sits beside the existing Cobra CLI and uses the same config, cache, and query transport. The target is a read-only browser flow:

1. namespace list
2. repo list inside a selected namespace
3. repo explorer with endpoint list plus a detail pane with `Endpoints`, `Servers`, and `Errors` tabs

The implementation will use `bubbletea`, `bubbles`, `glamour`, `viewport`, and `lipgloss` v2. The TUI will reuse the current CLI service instead of introducing a second client stack.

## Scope
In scope:
- add a `shiva tui` subcommand to the existing CLI binary
- implement three navigation stages:
  - namespaces
  - repos in one namespace
  - repo explorer
- render selected endpoint details as markdown in a scrollable pane
- render selected endpoint server information and documented error responses in separate tabs
- reuse existing profile, target, offline, and catalog/cache behavior through the current CLI service
- support resize-aware layout and deterministic async loading

Out of scope:
- replacing the current shorthand CLI or `ls` flows
- editing config, mutating server state, or dispatching calls from the TUI
- inventing a second transport layer separate from `internal/cli`
- adding server-side endpoints only for the TUI
- generic fuzzy finder workflows outside the described navigation

## Why This Is Needed
The current CLI is efficient for direct selectors and scriptable output, but it does not provide an interactive browsing surface for discovery across namespaces, repos, and endpoints.

Observed current state:
- `cmd/shiva/main.go` builds one Cobra command tree around `internal/cli.NewRootCommand`
- `internal/cli/root.go` owns the shorthand entrypoint plus `ls`, `sync`, `batch`, `completion`, and `health`
- `internal/cli/list_command.go` already implements the same information hierarchy as text output:
  - namespaces
  - repos within a namespace
  - operations within a repo
- `internal/cli/service.go` and `internal/cli/service_inventory.go` already provide the data the TUI needs:
  - repo inventory
  - operation inventory
  - raw operation bodies
  - raw spec bodies
- `internal/cli/httpclient/client.go` already maps those reads to `/v1/repos`, `/v1/operations`, `/v1/operation`, and `/v1/spec`
- `docs/cli.md` and `docs/endpoints.md` define the current CLI and query contracts that the TUI must not fork

What is missing is not data access. What is missing is an interactive, width-aware terminal surface over the current read model.

## Goals
- Keep one CLI binary with one config/cache model.
- Reuse current read/query service code instead of duplicating HTTP transport logic.
- Make namespace, repo, and endpoint discovery possible without memorizing selectors.
- Keep the repo explorer deterministic under resize and rapid selection changes.
- Render endpoint detail as readable markdown, not raw JSON.
- Keep the first TUI implementation read-only and bounded.

## Non-goals
- Adding outbound call execution UI.
- Adding write paths for profiles, targets, or cache management.
- Building a generic pane framework for future unrelated screens.
- Supporting every existing CLI selector form as a TUI entry selector.
- Introducing server-owned markdown payloads.

## Current Baseline (Observed)
CLI command ownership today:
- `internal/cli/root.go` creates the root Cobra command and shared flags.
- `internal/cli/list_command.go` resolves the same namespace -> repo -> operation hierarchy, but renders plain text.
- `internal/cli/root_test.go` covers shorthand dispatch and subcommand wiring.

Service and transport behavior today:
- `internal/cli/service.go` exposes `GetSpec`, `GetOperation`, `ListRepos`, `ListAPIs`, and `ListOperations`.
- `internal/cli/service_inventory.go` fetches repo, API, and operation inventories through the shared catalog/cache layer.
- `internal/cli/catalog/service.go` already owns floating refresh, offline reuse, and pinned snapshot behavior.
- `internal/cli/httpclient/client.go` fetches:
  - `/v1/repos`
  - `/v1/apis`
  - `/v1/operations`
  - `/v1/operation`
  - `/v1/spec`

Data already available to a read-only TUI:
- repo inventory rows via `output.RepoRow`
- operation inventory rows via `output.OperationRow`
- raw canonical operation JSON via `/v1/operation`
- raw canonical spec JSON or YAML via `/v1/spec`

Presentation baseline:
- `internal/cli/output/list.go` defines the JSON row shapes the TUI can decode
- `internal/cli/list_style.go` already shows that terminal styling is isolated from service logic
- `go.mod` already includes `charm.land/lipgloss/v2`

There is no current TUI package, no markdown builder for OpenAPI operation views, and no interactive state model.

## Target Contract or Target Architecture
Shiva will add one read-only TUI surface with one explicit ownership split.

### Command Contract
The CLI will add a new subcommand:
- `shiva tui`

Initial selector support:
- `shiva tui`
  - starts at namespace list
- `shiva tui <namespace>/`
  - starts at repo list for that namespace
- `shiva tui <namespace>/<repo>`
  - starts at repo explorer for that repo

The TUI command will accept only flags that make sense for read-only browsing:
- `--profile`
- `--offline`

It will not accept:
- `--api`
- `--sha`
- `--rev`
- `--via`
- `--dry-run`
- request body / header / query input flags

The existing shorthand CLI behavior remains unchanged.

### Package Boundaries
Add:
- `internal/tui`
- `internal/tui/markdown`

Keep ownership:
- `internal/cli`
  - Cobra command wiring
  - config path loading
  - shared service construction
- `internal/tui`
  - Bubble Tea model, update loop, keyboard handling, async command wiring, and layout
- `internal/tui/markdown`
  - build markdown source strings for endpoint, server, and error views
- `internal/cli/service.go` and `internal/cli/service_inventory.go`
  - remain the only data access layer used by the TUI

The TUI package will depend on a narrow read-only interface, not on Cobra:

```go
type BrowserService interface {
	ListRepos(ctx context.Context, options cli.RequestOptions, format output.ListFormat) ([]byte, error)
	ListOperations(ctx context.Context, selector request.Envelope, options cli.RequestOptions, format output.ListFormat) ([]byte, error)
	GetOperation(ctx context.Context, selector request.Envelope, options cli.RequestOptions) ([]byte, error)
	GetSpec(ctx context.Context, selector request.Envelope, options cli.RequestOptions, format cli.SpecFormat) ([]byte, error)
}
```

`cli.Service` already satisfies this subset, so the command layer can pass the existing service directly.

### Screen Model
The TUI will use one root model with three route states:
- `routeNamespaces`
- `routeRepos`
- `routeRepoExplorer`

State owned by the root model:
- active route
- current profile/offline options
- loaded repo rows
- selected namespace
- selected repo row
- namespace list model
- repo list model
- endpoint list model
- active detail tab:
  - `Endpoints`
  - `Servers`
  - `Errors`
- viewport model for right pane
- current window width and height
- async load state and last error per domain

The model will not create parallel mutable submodels for the same slice of data. One route owns one list at a time.

### Navigation Contract
Stage 1: namespace list
- source: one `ListRepos(..., json)` load
- rows are grouped into namespace summary entries in memory
- `enter` opens repo list for the selected namespace

Stage 2: repo list
- source: already loaded repo rows filtered by namespace
- `enter` opens repo explorer for the selected repo
- `esc` returns to namespace list

Stage 3: repo explorer
- source:
  - one `ListOperations(..., json)` load for the selected repo
  - one lazy `GetOperation` load for the selected endpoint
  - one lazy `GetSpec(..., json)` load for the selected endpoint's owning API when server detail needs spec-level servers
- `up` and `down` move inside endpoint list
- `tab` and `shift+tab` switch right-pane tabs
- `esc` returns to repo list
- `q` or `ctrl+c` quits from any stage

Repo explorer layout:
- header with `<namespace>/<repo>`
- tab row with `Endpoints`, `Servers`, `Errors`
- left pane: endpoint list
- right pane: markdown viewport

Responsive rule:
- wide terminals use horizontal split
- narrow terminals switch to vertical stacking
- width and height changes reflow list sizes and rerender markdown

### Data and Selection Rules
Repo explorer endpoint list:
- loads operation inventory without an `api` filter so it can cover multi-API repos
- sorts rows deterministically by:
  - `path`
  - `method`
  - `operation_id`
  - `api`
- left list labels show:
  - method
  - normalized path with `:param`
  - optional `#operationId`
  - API suffix when the repo contains more than one API

Selected endpoint identity:
- namespace
- repo
- API
- either `operation_id` or `method` plus `path`

The selected endpoint row is the authority for all right-pane loads. The TUI does not infer a different API or endpoint from tab state.

### Detail Tab Contract
`Endpoints` tab:
- right pane shows the full selected endpoint as markdown
- content includes:
  - method and path header
  - operation id
  - summary
  - description
  - deprecated marker when present
  - parameters
  - request body
  - success and error responses

`Servers` tab:
- right pane shows server information for the selected endpoint
- source precedence:
  - operation-level `servers`
  - otherwise owning spec `servers`
- when neither exists, show an empty-state markdown block

`Errors` tab:
- right pane shows only documented non-`2xx` responses and `default` from the selected operation
- if none exist, show an empty-state markdown block

This keeps the left pane stable across tabs and avoids inventing a second list domain for servers or errors.

### Markdown Rendering Contract
The TUI will not render raw JSON directly. It will:

1. decode operation/spec JSON into generic maps or typed OpenAPI fragments
2. build markdown source in `internal/tui/markdown`
3. render markdown to ANSI with `github.com/charmbracelet/glamour`
4. display the rendered ANSI text in `github.com/charmbracelet/viewport`

The markdown builders will follow the structure of the reference renderer in `/Users/v.v.kovalev/@v.v.kovalev/services/src/markdown`:
- endpoint/service metadata at the top
- parameter and body schemas rendered as code fences
- responses grouped by status code

Required builders:
- `BuildEndpoint(operationRow, operationJSON, specMeta) string`
- `BuildServers(operationJSON, specJSON) string`
- `BuildErrors(operationJSON) string`

Renderer rules:
- markdown render width equals viewport content width
- viewport content is regenerated on window resize
- renderer creation is centralized so style setup is not duplicated across tabs

### Bubble Tea and Charm Stack
The implementation stack is fixed to:
- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`
- `github.com/charmbracelet/glamour`
- `github.com/charmbracelet/viewport`
- `charm.land/lipgloss/v2`

Component choices:
- `bubbles/list` for namespace, repo, and endpoint lists
- `bubbles/help` for compact key hints
- `viewport` for the right detail pane
- `lipgloss` v2 for layout, borders, spacing, tabs, status lines, and empty/error states

The TUI must not mix ad hoc terminal escape assembly into business logic.

### Async and Race Rules
Race conditions will be handled by determinism, not sleeps.

Each async load domain gets a monotonic request token:
- repo catalog load
- repo operations load
- endpoint detail load
- spec detail load

Rules:
- selection change increments the relevant token before dispatch
- completion messages carry the token they were started with
- the model applies a response only when the token matches the latest active token
- stale responses are ignored
- tab switches never mutate endpoint selection

This prevents older network or cache responses from overwriting newer selection state.

### Error and Empty-State Rules
Load failures remain local to the screen that requested them.

Rules:
- namespace-stage repo load failure blocks entry and shows one recoverable full-screen error state
- repo explorer operation-load failure blocks only the explorer body
- endpoint markdown failure keeps the endpoint list usable and shows an error panel on the right
- empty lists render explicit empty-state text instead of blank panes
- offline cache misses surface the existing CLI error messages unchanged

## Implementation Notes
### Command Wiring
Add `newTUICommand` under `internal/cli` and register it from `internal/cli/root.go`.

The command will:
- validate allowed flags
- load the shared service through the same `serviceFactory`
- create a TUI program with a narrow browser-service dependency
- map command args to the initial route

`cmd/shiva/main.go` remains responsible for config and cache path resolution only once.

### Data Loading Strategy
Startup:
- load repo inventory once
- derive namespace entries from repo rows in memory

Entering repo explorer:
- load operation inventory once for the repo
- select the first endpoint row by default when the list is non-empty
- dispatch endpoint-detail load immediately for the selected row

Opening `Servers` tab:
- if the selected operation has operation-level servers, render without spec fetch
- otherwise fetch the selected API spec lazily and cache it in memory by API key

Opening `Errors` tab:
- reuse the already fetched operation detail
- no extra network request

Selection changes:
- keep a per-endpoint detail cache in memory for the current repo explorer session
- if detail exists for the selected endpoint, rerender locally
- otherwise fetch once and cache

This keeps the TUI responsive without introducing a new persistent cache tier.

### State Types
Use explicit view-state structs instead of one untyped map.

At minimum:
- `NamespaceEntry`
- `RepoEntry`
- `EndpointEntry`
- `OperationDetail`
- `SpecDetail`
- `DetailTab`
- typed Bubble Tea messages for each async completion path

### Rendering Notes
Header content should include:
- repo path
- selected tab
- profile name when non-default
- offline marker when enabled

Footer/help should include only active keys for the current route.

The first implementation should prefer plain, deterministic styling over a heavy custom theme. `lipgloss` usage should stay in one styling module.

### Test Strategy
Add focused tests before package-wide tests.

Required coverage:
- `internal/cli`
  - `tui` subcommand wiring
  - flag validation
  - selector-to-initial-route mapping
- `internal/tui`
  - namespace -> repo -> explorer transitions
  - tab switching
  - stale async response rejection by token
  - empty and error states
- `internal/tui/markdown`
  - endpoint markdown builder
  - server markdown builder
  - error-response markdown builder

The TUI logic should be testable through model updates without requiring an interactive terminal in tests.

## Milestones
### Milestone 1
Scope:
- add `shiva tui`
- add root model and three-stage navigation
- load repo and operation inventories through existing CLI service
- render repo explorer right pane as temporary plain text

Expected results:
- users can browse namespaces, repos, and endpoints inside one TUI session

Testable outcome:
- model tests cover route transitions and operation list loading, and `shiva tui` launches successfully from the Cobra tree

### Milestone 2
Scope:
- add markdown builders
- integrate `glamour` and `viewport`
- implement `Endpoints`, `Servers`, and `Errors` tabs
- add resize-aware rerendering

Expected results:
- selected endpoint details render as readable markdown with scrollable content

Testable outcome:
- markdown builder tests pass and model tests confirm tab-specific content plus resize-triggered rerender

### Milestone 3
Scope:
- add detail caching, key help, and polish
- document shipped TUI behavior in `docs/cli.md`
- run focused CLI and TUI tests, then full suite

Expected results:
- TUI browsing remains responsive across repeated selections and has a documented operator contract

Testable outcome:
- repeated selection changes reuse cached detail when available, docs reflect the shipped command, and `go test ./...` passes

## Acceptance Criteria
- `shiva tui` is available beside the existing CLI commands.
- The TUI reuses the existing CLI service and query endpoints.
- The initial navigation flow is namespace list -> repo list -> repo explorer.
- Repo explorer keeps the endpoint list on the left and a tabbed detail pane on the right.
- `Endpoints` shows full selected-operation markdown.
- `Servers` shows operation-level or owning-spec server info for the selected endpoint.
- `Errors` shows only documented error responses for the selected endpoint.
- Resize and rapid selection changes do not apply stale async results.
- No second HTTP client or config system is introduced for the TUI.

## Risks
- `glamour` width-dependent output may require rerender throttling discipline to avoid excessive work on repeated resize events.
- Multi-API repos can make left-list labels noisy if API identity is not shown clearly.
- Fetching spec detail on demand for server rendering can feel inconsistent if cache and network latency differ sharply.
- Bubble Tea component behavior can become hard to test if command wiring and view logic are mixed.

## References
- [README](../README.md)
- [CLI behavior](../docs/cli.md)
- [Endpoint and query contract](../docs/endpoints.md)
- [Setup and runtime files](../docs/setup.md)
- [Testing strategy](../docs/testing.md)
- [Existing validation architecture DD](./openapi-validation-architecture.md)
- `cmd/shiva/main.go`
- `internal/cli/root.go`
- `internal/cli/list_command.go`
- `internal/cli/service.go`
- `internal/cli/service_inventory.go`
- `internal/cli/httpclient/client.go`
- `/Users/v.v.kovalev/@v.v.kovalev/services/src/markdown/renderService.ts`
- `/Users/v.v.kovalev/@v.v.kovalev/services/src/markdown/renderRequestBody.ts`
- `/Users/v.v.kovalev/@v.v.kovalev/services/src/markdown/renderResponses.ts`
- `/Users/v.v.kovalev/@v.v.kovalev/services/src/markdown/renderParamsArray.ts`
