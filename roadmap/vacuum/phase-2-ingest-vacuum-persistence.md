# Vacuum Integration Phase 2

Scope: Run vacuum in the ingestion path after canonical persistence and store normalized issues per processed spec revision.

Documentation: `design/vacuum.md`

Legend: [ ] todo, [x] done.

- [x] 2.1 Add the canonical-spec vacuum adapter
  - Repository: `shiva`
  - Component: `internal/openapi/lint`
  - Verification: canonical spec input produces normalized rule-backed issues and parser failures are normalized separately
  - Reasoning: high
1. Add `internal/openapi/lint` types for normalized vacuum issues and canonical-spec execution results.
2. Integrate the pinned vacuum rule catalog and map native report output into Shiva issue rows with `rule_id`, `message`, `json_path`, and four-number range.
3. Keep `origin.absoluteLocation` transient and exclude it from the persistent issue model.

- [x] 2.2 Wire vacuum execution into revision processing
  - Repository: `shiva`
  - Component: `cmd/shivad`, `internal/store`
  - Verification: processed revisions persist issues, zero-issue revisions stay clean, failed vacuum runs mark the revision as failed
  - Reasoning: high
1. Update `cmd/shivad` to set `api_spec_revisions.vacuum_status='processing'` after `PersistCanonicalSpec`.
2. Run vacuum against the canonical spec for the current `api_spec_revision_id` and replace its stored issue set in one revision-scoped write path.
3. Set `vacuum_status`, `vacuum_error`, and `vacuum_validated_at` deterministically on success and failure.

- [x] 2.3 Extend processor tests for vacuum persistence behavior
  - Repository: `shiva`
  - Component: `cmd/shivad`
  - Verification: processor tests cover success, zero-issue success, and vacuum execution failure without regressing canonical persistence
  - Reasoning: medium
1. Extend revision processor fakes to record vacuum state and persisted issue writes.
2. Add tests for a processed revision that yields non-empty issue rows.
3. Add tests for zero-issue and vacuum-failure outcomes and assert final revision state fields.
