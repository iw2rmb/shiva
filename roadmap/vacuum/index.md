# Vacuum Integration

Scope: Implement the `vacuum` design so Shiva stores lint rules and issues per spec revision and provides immediate GitLab CI validation output for merge request workflows.

Documentation: `design/vacuum.md`

Legend: [ ] todo, [x] done.

- [ ] 1.0 Phase 1 schema and store foundation
  - Repository: `shiva`
  - Component: `sql/schema`, `sql/query`, `internal/store`, `internal/store/sqlc`
  - Verification: fresh schema bootstrap seeds `vacuum_rules`, store tests persist and replace `vacuum_issues`, revision lint state round-trips
  - Reasoning: high
1. Implement the schema changes from `design/vacuum.md` by editing `sql/schema/000001_initial.sql` directly with `CREATE` statements and seeded `INSERT`s, without `ALTER`.
2. Add sqlc query files and store methods for `vacuum_rules`, `vacuum_issues`, and `api_spec_revisions` vacuum state.
3. Regenerate `internal/store/sqlc` and cover the new store primitives with focused tests.

- [ ] 2.0 Phase 2 ingestion-time vacuum persistence
  - Repository: `shiva`
  - Component: `cmd/shivad`, `internal/openapi/lint`, `internal/store`
  - Verification: processed revisions persist all issues, zero-issue revisions set `vacuum_status='processed'`, vacuum failures set `vacuum_status='failed'`
  - Reasoning: high
1. Add the `internal/openapi/lint` vacuum adapter for canonical-spec execution and normalized issue mapping.
2. Wire `cmd/shivad` revision processing to run vacuum after `PersistCanonicalSpec` and replace persisted issue rows for the target `api_spec_revision_id`.
3. Extend processor tests to cover success, zero-issue, and vacuum-failure paths.

- [x] 3.0 Phase 3 GitLab CI validation surface
  - Repository: `shiva`
  - Component: `internal/http`, `internal/openapi/lint`, `internal/gitlab`, `cmd/shivad`
  - Verification: CI endpoint returns empty results when no spec is impacted, returns source-file-localized issues when specs are impacted, emits GitLab Code Quality-compatible payloads
  - Reasoning: xhigh
1. Add `POST /internal/gitlab/ci/validate` and its request and response contracts in `internal/http`.
2. Reuse current compare and root-resolution logic to reconstruct source-layout documents for the requested SHA and run transient vacuum validation against them.
3. Map transient lint results into Shiva JSON and GitLab Code Quality-compatible output and cover the route with HTTP tests.
