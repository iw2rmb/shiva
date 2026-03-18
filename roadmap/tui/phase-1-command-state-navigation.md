# Shiva TUI Phase 1

Scope: Add the `shiva tui` command, the root Bubble Tea model, and the namespace -> repo -> endpoint navigation flow using the existing CLI service.

Documentation: [TUI Architecture](../../design/tui.md)

Legend: [ ] todo, [x] done.

- [x] 1.1 Add TUI command wiring and flag validation
  - Repository: `shiva`
  - Component: `internal/cli`, `cmd/shiva`
  - Verification: `shiva tui` starts, `shiva tui acme/` starts in repo view, invalid flags are rejected with CLI validation errors
  - Reasoning: medium
1. Add `newTUICommand` in `internal/cli` and register it from `internal/cli/root.go`.
2. Reuse the existing `serviceFactory` and pass a narrow browser-service dependency into the TUI package.
3. Parse `shiva tui`, `shiva tui <namespace>/`, and `shiva tui <namespace>/<repo>` into explicit initial route inputs.
4. Reject unsupported flags for `tui` in the same style as `ls` and `health`.
5. Add command-level tests in `internal/cli` for valid entry forms and invalid flag combinations.

- [x] 1.2 Define typed TUI state and async message contracts
  - Repository: `shiva`
  - Component: `internal/tui`
  - Verification: model construction compiles, request tokens are carried in typed messages, stale completion messages can be ignored in tests
  - Reasoning: high
1. Create `internal/tui` with typed route, tab, entry, and detail state structs.
2. Define the narrow browser-service interface from the design doc and adapt the root command to use it.
3. Add typed Bubble Tea messages for repo catalog, operation list, operation detail, spec detail, resize, and failure paths.
4. Add monotonic request tokens per async load domain and store the latest active token in the model.
5. Add focused model tests that prove stale async results do not overwrite newer selection state.

- [x] 1.3 Implement namespace and repo list screens
  - Repository: `shiva`
  - Component: `internal/tui`
  - Verification: startup loads namespaces, `enter` opens repos, `esc` returns to namespaces, empty and load-failure states render deterministically
  - Reasoning: medium
1. Load repo inventory once through `ListRepos(..., json)` when the TUI starts.
2. Derive namespace summary entries in memory from the loaded repo rows.
3. Build namespace and repo screens with `bubbles/list` and route-specific key handling.
4. Keep list sizing and styling separate from the service and state-transition code.
5. Add model tests for namespace selection, repo filtering, empty catalogs, and startup load errors.

- [x] 1.4 Implement repo explorer with endpoint list and placeholder detail pane
  - Repository: `shiva`
  - Component: `internal/tui`
  - Verification: entering a repo loads operations, `up` and `down` change endpoint selection, placeholder detail updates with selected endpoint identity
  - Reasoning: high
1. Enter repo explorer by loading `ListOperations(..., json)` for the selected repo without an `api` filter.
2. Sort endpoint rows deterministically by `path`, `method`, `operation_id`, and `api`.
3. Build the explorer layout with repo header, tab row, left endpoint list, and right placeholder pane.
4. Select the first endpoint row by default and reload the right pane when selection changes.
5. Add model tests for explorer entry, endpoint movement, back navigation, and empty operation catalogs.
