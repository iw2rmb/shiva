# Vacuum Integration Phase 1

Scope: Add the schema and store foundation for the vacuum rule catalog, revision lint state, and persisted issues.

Documentation: `design/vacuum.md`

Legend: [ ] todo, [x] done.

- [ ] 1.1 Add vacuum schema objects and seeded rules
  - Repository: `shiva`
  - Component: `sql/schema`
  - Verification: fresh database bootstrap creates `vacuum_rules`, `vacuum_issues`, and seeded rule rows
  - Reasoning: high
1. Edit `sql/schema/000001_initial.sql` directly and add `vacuum_rules` and `vacuum_issues` with `CREATE TABLE` statements.
2. Edit `sql/schema/000001_initial.sql` directly and add `vacuum_status`, `vacuum_error`, and `vacuum_validated_at` to `api_spec_revisions` in the original `CREATE TABLE`, without `ALTER`.
3. Seed the full built-in `vacuum all` rule catalog in `sql/schema/000001_initial.sql` with `recommended` and `formats` removed from the stored payload.

- [ ] 1.2 Add store queries and types for rules, issues, and revision lint state
  - Repository: `shiva`
  - Component: `sql/query`, `internal/store`, `internal/store/sqlc`
  - Verification: store primitives create, replace, list, and delete issue rows and update revision vacuum state
  - Reasoning: high
1. Add sqlc queries for listing seeded rules, replacing `vacuum_issues` for one `api_spec_revision_id`, and updating `api_spec_revisions` vacuum state.
2. Add `internal/store` methods and types for the new rule and issue shapes, including four-number `range_pos` validation.
3. Regenerate `internal/store/sqlc` and keep the new store API narrow to vacuum-specific behavior.

- [ ] 1.3 Cover the schema and store behavior with tests
  - Repository: `shiva`
  - Component: `internal/store`
  - Verification: store tests cover seeded-rule presence, issue replacement, zero-issue replacement, and vacuum state transitions
  - Reasoning: medium
1. Extend fresh-schema bootstrap tests to assert seeded `vacuum_rules` rows exist.
2. Add store tests for replacing persisted issue rows by `api_spec_revision_id`.
3. Add store tests for setting and reading `api_spec_revisions` vacuum status and error fields.
