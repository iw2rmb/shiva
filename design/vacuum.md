# Vacuum Integration

## Summary
Shiva will integrate `vacuum` as the static OpenAPI lint engine.

The design has three concrete outcomes:
- store the full built-in `vacuum` rule catalog in the database during the initial schema bootstrap
- validate every processed OpenAPI spec update and persist normalized issues per `api_spec_revision_id`
- add a GitLab CI-facing validation feature that can return source-file-localized issues immediately for merge request UI workflows

Persisted issue storage is revision-scoped and canonical-spec-based. GitLab CI display is source-file-based and on-demand.

## Scope
In scope:
- database schema for the `vacuum` rule catalog
- database schema for revision-scoped `vacuum` issue persistence
- ingestion-time `vacuum` execution for processed API spec revisions
- CI-triggered `vacuum` validation for GitLab merge request workflows
- normalized result mapping from `vacuum` report output into Shiva-owned records
- HTTP surface for GitLab CI validation responses

Out of scope:
- replacing the current runtime request or response validation stack
- storing `origin.absoluteLocation`
- storing `recommended` or `formats` from `vacuum` rules
- generic issue-query endpoints for CLI or UI outside GitLab CI
- posting directly back to GitLab APIs from Shiva
- non-OpenAPI lint engines

## Why This Is Needed
Shiva already builds and persists canonical specs, endpoints, and semantic diffs, but it does not yet persist lint findings or expose them for review workflows.

Current gaps:
- no persistent lint rule catalog
- no per-spec-update lint issue storage
- no validation status on `api_spec_revisions`
- no GitLab CI feature for returning issues in a merge-request-friendly format

The `vacuum` sampling note in `research/vacuum.md` also showed two important facts:
- `vacuum` is viable on Shiva-managed specs and produces stable rule IDs and structured reports
- the default ruleset is noisy, and one stored "processed" artifact was not OpenAPI at all

That means Shiva needs both:
- durable storage of all findings for each processed spec update
- a controlled integration surface that can revalidate and format issues for GitLab review without broadcasting raw tool output blindly

## Goals
- Persist the full built-in `vacuum` rule catalog in the schema bootstrap.
- Run `vacuum` on every processed API spec revision created by ingestion.
- Persist all `vacuum` issues per `api_spec_revision_id`, including zero-issue successful validations via revision status fields.
- Store issue ranges as a fixed four-number array.
- Keep persistent issue storage independent from transient absolute temp-file paths.
- Provide a GitLab CI-facing feature that returns source-file-localized issues immediately for merge request display.
- Reuse the existing GitLab resolver and spec-discovery logic instead of building a second spec discovery path.

## Non-goals
- Generalizing the schema to support every future lint engine now.
- Replacing Shiva semantic diff storage with `vacuum`.
- Running `vacuum` in the `/gl/*` request path.
- Building a full review UI in Shiva.
- Solving database upgrade compatibility beyond the repository's current initial-migration model.

## Current Baseline (Observed)
Ingestion and canonical persistence already exist:
- `cmd/shivad/main.go` runs canonical build in `revisionProcessor.runBuildStage`.
- `cmd/shivad/main.go` persists canonical artifacts through `store.PersistCanonicalSpec`.
- `internal/store/spec_artifacts.go` upserts `spec_artifacts` and replaces `endpoint_index` in one transaction.
- `cmd/shivad/main.go` computes semantic diffs after build with `openapi.ComputeSemanticDiff`.

GitLab-backed source resolution already exists:
- `internal/openapi/resolver.go` resolves root documents and local `$ref` closures from GitLab-backed repository files.
- bootstrap and incremental flows in `docs/gitlab.md` already identify impacted roots and rebuild them at a target SHA.

Current schema does not have lint storage:
- `sql/schema/000001_initial.sql` contains `api_specs`, `api_spec_revisions`, `spec_artifacts`, `endpoint_index`, and `spec_changes`
- there is no rule catalog table
- there is no issue table
- `api_spec_revisions` has `build_status` and `error`, but no lint/validation status

Current HTTP surface does not have validation integration endpoints:
- `internal/http/server.go` registers `/healthz`, `/internal/webhooks/gitlab`, `/gl/*`, and `/v1/{spec,operation,call,apis,operations,repos,catalog/status}`
- there is no GitLab CI validation route

Current migration model matters:
- `internal/store/migrate.go` enforces one checksum-locked initial migration
- `docs/database.md` states schema changes are made by editing `sql/schema/000001_initial.sql`
- changing seeded rule data changes the schema checksum and therefore changes the expected bootstrap state

## Target Contract or Target Architecture
### 1. Rule Catalog
Shiva stores the full built-in `vacuum` rule catalog in the database.

Rule source:
- the catalog comes from the built-in `vacuum all` ruleset of the embedded tool version Shiva ships with
- `recommended` and `formats` are intentionally not stored

Schema:
- table: `vacuum_rules`
- stable key: `rule_id`
- columns:
  - `rule_id`
  - `severity`
  - `type`
  - `category_id`
  - `category_name`
  - `description`
  - `how_to_fix`
  - `given_path`
  - `rule_json`

Rules:
- `rule_json` stores the normalized `vacuum` rule object with `recommended` and `formats` removed
- `rule_id` is the foreign-key target for persisted issues
- the initial migration seeds all rows

### 2. Revision-Scoped Issue Storage
Shiva stores `vacuum` issues per processed API spec revision.

Schema:
- table: `vacuum_issues`
- key scope: one issue row belongs to exactly one `api_spec_revision_id`
- columns:
  - `id`
  - `api_spec_revision_id`
  - `rule_id`
  - `message`
  - `json_path`
  - `range_pos`
  - `created_at`

Range contract:
- `range_pos` is an integer array of exactly four elements
- order is:
  - `[start_line, start_character, end_line, end_character]`

Rules:
- no `origin.absoluteLocation` is stored
- issue rows are replaced as a full set when the same revision is revalidated
- issue rows persist all severities, not only errors or warnings

Revision state:
- `api_spec_revisions` gains:
  - `vacuum_status`
  - `vacuum_error`
  - `vacuum_validated_at`

Status values:
- `pending`
- `processing`
- `processed`
- `failed`

Meaning:
- `processed` means `vacuum` completed successfully, even if zero issues were found
- `failed` means `vacuum` execution or parsing failed for that revision and `vacuum_error` explains why

### 3. Ingestion-Time Vacuum Execution
`vacuum` runs after canonical artifact persistence for every successfully built API revision.

Execution authority:
- lint the canonical spec artifact for the revision

Why canonical here:
- deterministic input
- stable revision-scoped storage
- no dependence on temp file locations
- results match the persisted canonical contract Shiva serves elsewhere

Rules:
- `vacuum` runs only for `api_spec_revisions` that reach `build_status='processed'`
- revisions that fail before canonical build continue using existing `api_spec_revisions.error`
- Shiva stores all `vacuum` findings for processed revisions, not only a filtered subset

### 4. GitLab CI Validation Feature
Shiva adds a CI-facing HTTP feature for immediate merge request validation.

Route:
- `POST /internal/gitlab/ci/validate`

Purpose:
- validate changed or impacted OpenAPI roots for a specific GitLab repo SHA
- return source-file-localized issues suitable for GitLab CI job consumption and MR display

Request contract:
- `gitlab_project_id`
- `namespace`
- `repo`
- `sha`
- `branch`
- optional `parent_sha`
- optional `format`
  - `shiva`
  - `gitlab_code_quality`

Behavior:
- resolve impacted or discovered roots for the requested SHA using the same GitLab compare/discovery rules already used by the worker
- if no spec is involved, return an empty successful response
- if specs are involved, re-run `vacuum` on source-layout files reconstructed from GitLab content for that SHA
- use the source-layout result only for the response payload
- do not persist the transient source-file absolute locations

Why this is separate from persisted revision issues:
- persisted issue ranges are canonical-spec-local
- GitLab MR display needs source-file-local paths and lines
- source revalidation gives meaningful file mapping without polluting stored revision records with temp paths

Output contract:
- default format `shiva`
  - grouped by spec root
  - includes issue rows with `rule_id`, severity, message, json path, repo-relative file path, and four-number range
- `gitlab_code_quality`
  - emits GitLab Code Quality-compatible issue objects derived from the same revalidation run
  - deterministic fingerprint is derived from repo, root path, rule ID, json path, range, and message

### 5. Separation of Persistent and Transient Results
Shiva keeps two distinct products of `vacuum`:

Persistent results:
- canonical-spec-based
- stored in `vacuum_issues`
- keyed by `api_spec_revision_id`

Transient CI results:
- source-file-based
- returned directly from `/internal/gitlab/ci/validate`
- not stored as separate rows

This split is mandatory because GitLab UI location fidelity and deterministic revision storage are different problems.

## Implementation Notes
### Package Ownership
Add under the lint layer:
- `internal/openapi/lint`

Suggested responsibilities:
- rule catalog snapshot and normalization
- canonical-spec lint execution
- source-layout lint execution for CI
- report-to-Shiva issue mapping

Keep current ownership:
- `cmd/shivad` owns ingest orchestration and calls lint after build persistence
- `internal/openapi` continues to own resolver and canonical build behavior
- `internal/http` owns the CI-facing route
- `internal/store` owns persistent rule and issue storage

### Database Shape
Update the original schema in `sql/schema/000001_initial.sql` directly. Do not add `ALTER` statements or a new migration file.

Update `sql/schema/000001_initial.sql` to add:
- `vacuum_rules`
- `vacuum_issues`
- `api_spec_revisions.vacuum_status`
- `api_spec_revisions.vacuum_error`
- `api_spec_revisions.vacuum_validated_at`

Recommended indexes:
- `vacuum_issues(api_spec_revision_id, rule_id)`
- `vacuum_issues(rule_id)`
- `api_spec_revisions(vacuum_status, id)`

Recommended constraints:
- `vacuum_status` limited to `pending|processing|processed|failed`
- `range_pos` array length must equal `4`
- `rule_id` references `vacuum_rules(rule_id)`

### Seeding Rules
The initial migration seeds the rule catalog.

Source-of-truth rule set:
- full built-in `vacuum all` catalog for the pinned Shiva vacuum version

Normalization before seeding:
- drop `recommended`
- drop `formats`
- keep the remaining metadata in `rule_json`
- denormalize common fields into dedicated columns

Because the repository uses a checksum-locked initial migration, changing the seeded catalog is a schema change by definition.
That change is made by editing `sql/schema/000001_initial.sql` itself, not by layering `ALTER` statements.

### Ingestion Flow Changes
For each processed API revision:
1. build canonical spec
2. persist canonical artifact and endpoint index
3. set `api_spec_revisions.vacuum_status='processing'`
4. run `vacuum` on the canonical spec
5. replace all existing `vacuum_issues` rows for that revision
6. set:
   - `vacuum_status='processed'`
   - `vacuum_error=''`
   - `vacuum_validated_at=now()`
7. continue with semantic diff persistence and downstream processing

On lint execution failure:
- delete any partial `vacuum_issues` rows for the revision
- set `vacuum_status='failed'`
- set `vacuum_error` to the normalized failure message
- keep `build_status='processed'` because the canonical artifact itself was built successfully

### CI Validation Flow
The CI route should not wait for the async ingest worker.

Instead it:
1. accepts target repo and SHA from GitLab CI
2. determines whether spec files are impacted using the same compare/discovery logic used by the worker
3. reconstructs source-layout documents in a temp workspace from GitLab-fetched root and dependency files
4. runs `vacuum` on the source root path in that temp workspace
5. maps transient file locations back to repo-relative paths
6. returns either Shiva JSON or GitLab Code Quality output

This keeps MR feedback immediate and deterministic.

### Result Mapping
Persistent issue mapping stores:
- `rule_id`
- `message`
- `json_path`
- `range_pos`

Transient CI mapping additionally derives:
- repo-relative file path
- GitLab Code Quality location shape when requested

`origin.absoluteLocation` is used only as a transient mapping aid in the CI request path when available. It is never persisted.

### Broadcast and Future Read Surfaces
This DD does not add generic list/query endpoints for stored issues.

However, persisted issue rows are designed so future work can add:
- repo snapshot issue listing
- webhook payload enrichment
- CLI or TUI reporting

without changing the stored shape.

## Milestones
### Milestone 1
Scope:
- add schema for `vacuum_rules`, `vacuum_issues`, and revision lint state
- seed the rule catalog in `000001_initial.sql`

Expected results:
- fresh databases contain the full rule catalog and revision rows can track `vacuum` processing state

Testable outcome:
- store tests can create a fresh schema, verify seeded rules exist, and verify issue rows reference them

### Milestone 2
Scope:
- integrate canonical-spec `vacuum` execution into the ingestion processor
- persist and replace issue rows per `api_spec_revision_id`

Expected results:
- every successfully built API revision receives `vacuum_status`
- processed revisions can have zero or more persisted issues

Testable outcome:
- processor tests verify issue persistence, zero-issue success, and lint failure state transitions

### Milestone 3
Scope:
- add `/internal/gitlab/ci/validate`
- add GitLab Code Quality-compatible response mode

Expected results:
- GitLab CI can request immediate validation results for a target SHA without waiting for background ingest completion

Testable outcome:
- HTTP tests verify empty responses when no specs are impacted and source-file-localized issue responses when specs are impacted

## Acceptance Criteria
- the database contains a seeded `vacuum_rules` catalog on fresh bootstrap
- `recommended` and `formats` are not stored in the persistent rule catalog
- every processed `api_spec_revision_id` gets a `vacuum_status`
- successful zero-issue validations are represented as `vacuum_status='processed'` with no issue rows
- `vacuum_issues` rows store range positions as exactly four integers
- persisted issue rows do not store `origin.absoluteLocation`
- GitLab CI can request immediate validation results for a repo SHA and receive source-file-localized findings
- the CI feature can emit GitLab Code Quality-compatible output

## Risks
- the checksum-locked initial migration means rule catalog changes are schema changes; existing databases will not auto-upgrade.
- canonical-spec line numbers and source-file line numbers differ; using the wrong result type in the wrong surface would create misleading MR annotations.
- `vacuum` rule churn across versions can change the seeded catalog and issue set materially.
- source-layout CI validation duplicates some work from the ingest path, but this is the trade-off for immediate MR-localized feedback.
- very noisy built-in rules can overwhelm users if GitLab CI defaults are not curated in the response formatting layer.

## References
- [OpenAPI validation architecture](./openapi-validation-architecture.md)
- [Vacuum results](../research/vacuum.md)
- [OpenAPI validation frameworks](../research/openapi-validation-frameworks.md)
- [GitLab ingestion](../docs/gitlab.md)
- [Database](../docs/database.md)
- [Endpoints](../docs/endpoints.md)
- `cmd/shivad/main.go`
- `internal/openapi/resolver.go`
- `internal/store/migrate.go`
- `internal/store/spec_artifacts.go`
- `internal/http/server.go`
