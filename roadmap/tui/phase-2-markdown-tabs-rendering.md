# Shiva TUI Phase 2

Scope: Replace the placeholder detail pane with markdown-backed `Endpoints`, `Servers`, and `Errors` tabs rendered through `glamour` and `viewport`.

Documentation: [TUI Architecture](../../design/tui.md)

Legend: [ ] todo, [x] done.

- [x] 2.1 Add endpoint and spec detail loaders for the explorer
  - Repository: `shiva`
  - Component: `internal/tui`
  - Verification: selected endpoint loads raw operation detail, `Servers` lazily loads spec detail only when operation-level servers are absent, stale responses are ignored
  - Reasoning: high
1. Fetch selected endpoint detail with `GetOperation` using the exact selected row identity.
2. Cache operation detail in memory for the current explorer session by endpoint identity.
3. Fetch selected API spec detail with `GetSpec(..., json)` only when the `Servers` tab needs spec-level servers.
4. Cache spec detail in memory by repo and API identity for the current explorer session.
5. Add model tests for lazy spec loading, cache hits, and stale response rejection across rapid selection changes.

- [x] 2.2 Build markdown source generators for endpoint, servers, and errors
  - Repository: `shiva`
  - Component: `internal/tui/markdown`
  - Verification: endpoint markdown includes method, path, params, request body, and responses; servers markdown follows operation-first precedence; errors markdown filters to non-2xx and `default`
  - Reasoning: high
1. Add markdown builders for endpoint detail, server detail, and error detail in `internal/tui/markdown`.
2. Follow the structure of `/Users/vk/@iw2rmb/services/src/markdown/renderService.ts`, `renderRequestBody.ts`, `renderResponses.ts`, and `renderParamsArray.ts`.
3. Normalize OpenAPI parameter, request-body, and response sections into stable markdown blocks and fenced code sections.
4. Add empty-state markdown builders for missing servers and missing documented errors.
5. Add table-driven tests for markdown builders using representative operation and spec payloads.

- [x] 2.3 Integrate `glamour`, `viewport`, and tab-specific rendering
  - Repository: `shiva`
  - Component: `internal/tui`
  - Verification: `tab` and `shift+tab` switch rendered content, markdown is scrollable, rerender uses viewport width and updates on resize
  - Reasoning: medium
1. Add one centralized markdown renderer backed by `glamour`.
2. Render tab-specific markdown into ANSI text and place it inside a `viewport` detail pane.
3. Switch the right pane between `Endpoints`, `Servers`, and `Errors` without changing endpoint selection.
4. Recompute renderer width and viewport content when the terminal size changes.
5. Add model tests for tab switching, viewport content replacement, and resize-triggered rerendering.

- [x] 2.4 Finalize TUI layout and route-local help
  - Repository: `shiva`
  - Component: `internal/tui`
  - Verification: wide terminals use side-by-side layout, narrow terminals stack panes, route-local help reflects active keys only
  - Reasoning: medium
1. Add a single `lipgloss` styling module for header, tabs, panes, error blocks, and empty states.
2. Implement width-aware layout that switches between horizontal split and vertical stacking.
3. Add route-local help text with `bubbles/help` for namespace, repo, and explorer screens.
4. Keep rendering logic free of data-fetch and state-transition side effects.
5. Add model tests for layout mode switching and route-specific help output.
