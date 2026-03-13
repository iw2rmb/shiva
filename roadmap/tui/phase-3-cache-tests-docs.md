# Shiva TUI Phase 3

Scope: Finish explorer responsiveness, extend tests, and document the shipped TUI behavior in the long-lived docs.

Documentation: [TUI Architecture](../../design/tui.md)

Legend: [ ] todo, [x] done.

- [ ] 3.1 Harden detail caching and recoverable error handling
  - Repository: `shiva`
  - Component: `internal/tui`
  - Verification: revisiting an endpoint reuses cached detail, endpoint-list navigation stays usable after right-pane errors, offline cache misses surface existing CLI messages
  - Reasoning: medium
1. Reuse cached operation and spec detail before dispatching a new async load.
2. Keep right-pane failures local so the endpoint list remains interactive.
3. Preserve existing CLI error messages for offline cache misses and service failures.
4. Reset only the route-local error and loading fields when selection or tab changes require it.
5. Add model tests for cache reuse, recoverable right-pane errors, and offline error propagation.

- [ ] 3.2 Complete end-to-end command and model test coverage
  - Repository: `shiva`
  - Component: `internal/cli`, `internal/tui`
  - Verification: focused TUI tests pass, root command tests cover `tui`, package-level CLI and TUI tests pass
  - Reasoning: medium
1. Extend `internal/cli` tests to cover `tui` command registration, usage, and entry selector mapping.
2. Add table-driven model tests for full namespace -> repo -> explorer navigation flows.
3. Add regression tests for rapid endpoint changes, tab switching, and resize handling.
4. Run focused targets for `internal/cli` and `internal/tui` before broader package tests.
5. Run package-level tests for the modified areas after focused verification is green.

- [ ] 3.3 Document shipped TUI behavior and remove transient docs when appropriate
  - Repository: `shiva`
  - Component: `docs/cli.md`, `docs/testing.md`, `README.md`, `design/tui.md`, `roadmap/tui`
  - Verification: long-lived docs describe `shiva tui`, testing docs include focused TUI commands, doc links pass, transient design and roadmap are removed only after implementation is fully shipped and documented
  - Reasoning: low
1. Update `docs/cli.md` with the shipped `shiva tui` command, flags, navigation flow, and current limits.
2. Update `docs/testing.md` with focused TUI test commands.
3. Update `README.md` only if the shipped scope needs a top-level CLI summary change.
4. Run `~/@iw2rmb/auto/scripts/check_docs_links.sh` after doc updates.
5. Remove `design/tui.md` and `roadmap/tui` only after all roadmap items are complete and long-lived docs stand on their own.
