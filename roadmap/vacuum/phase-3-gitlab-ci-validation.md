# Vacuum Integration Phase 3

Scope: Add a GitLab CI-facing validation route that revalidates impacted specs from source layout and returns MR-friendly issue output immediately.

Documentation: `design/vacuum.md`

Legend: [ ] todo, [x] done.

- [x] 3.1 Add the GitLab CI validation route and contracts
  - Repository: `shiva`
  - Component: `internal/http`
  - Verification: the route validates input, accepts GitLab repo and SHA identity, and supports `shiva` and `gitlab_code_quality` formats
  - Reasoning: high
1. [x] Add `POST /internal/gitlab/ci/validate` to `internal/http/server.go`.
2. [x] Define request parsing and validation for `gitlab_project_id`, `namespace`, `repo`, `sha`, `branch`, optional `parent_sha`, and optional `format`.
3. [x] Define response writers for Shiva JSON and GitLab Code Quality-compatible payloads.

- [ ] 3.2 Reconstruct source-layout specs and run transient vacuum validation
  - Repository: `shiva`
  - Component: `internal/openapi/lint`, `internal/openapi`, `internal/gitlab`
  - Verification: impacted specs are revalidated from source layout, no-spec changes return an empty result, temp paths map back to repo-relative file paths
  - Reasoning: xhigh
1. Reuse current compare and root-discovery logic to detect whether the target SHA impacts any OpenAPI roots.
2. Reconstruct the root and dependency files in a temp workspace preserving repo-relative paths and run vacuum against the source root path.
3. Map transient file locations back to repo-relative paths and keep that mapping out of persistent storage.

- [ ] 3.3 Verify HTTP behavior end to end
  - Repository: `shiva`
  - Component: `internal/http`, `cmd/shivad`
  - Verification: HTTP tests cover no-spec, spec-hit, and GitLab Code Quality response scenarios
  - Reasoning: high
1. Add route tests for a SHA with no impacted specs and assert an empty success response.
2. Add route tests for a SHA with impacted specs and assert source-file-localized issue rows.
3. Add route tests for `format=gitlab_code_quality` and assert deterministic fingerprints and line mapping.
