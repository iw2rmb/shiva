# OpenAPI Validation Architecture

## Summary
Shiva needs one clear architecture for OpenAPI correctness checks. The design is:
- keep `kin-openapi` as the runtime validation authority
- add a static lint and policy layer based on `vacuum`
- add a separate breaking-change layer based on `oasdiff`

The expected outcome is one validation pipeline with explicit ownership boundaries, deterministic behavior, and no runtime dependency on Node-based tooling.

## Scope
In scope:
- document-level correctness checks for OpenAPI specs Shiva ingests
- static lint and policy checks during ingest and review
- revision-to-revision breaking-change analysis
- normalized result shapes for lint and breaking reports
- module ownership boundaries in Shiva

Out of scope:
- replacing the current `/gl/*` runtime validation stack
- generic uptime or ping monitoring against `servers` URLs
- documentation rendering
- non-OpenAPI protocol support
- a TUI or editor integration

## Why This Is Needed
Shiva already validates runtime requests and responses from stored OpenAPI snapshots, but it does not yet have one explicit architecture for broader spec correctness.

Today the codebase has:
- runtime validation in `internal/http/runtime_validation.go`, `internal/http/runtime_response.go`, and `internal/http/runtime_spec_cache.go`
- canonical extraction and OpenAPI file resolution in `internal/openapi/resolver.go` and `internal/openapi/inspect.go`
- semantic endpoint diffing in `internal/openapi/diff.go`

What is missing is not validation in general. What is missing is a clear split between:
- runtime correctness
- static lint and policy quality
- revision compatibility analysis

Without that split, Shiva risks mixing concerns and either:
- bolting lint logic into runtime paths, or
- picking one framework and forcing it to solve unrelated problems badly

## Goals
- Keep one runtime validation authority.
- Add static linting without introducing Node as a service dependency.
- Keep breaking-change analysis separate from structural validation.
- Run checks against canonical or otherwise deterministic spec inputs.
- Produce normalized findings that can power CLI output, webhooks, and future review UX.
- Preserve revision-aware behavior across all validation outputs.

## Non-goals
- Rewriting the resolver around a new model library.
- Replacing Shiva's existing semantic diff implementation in `internal/openapi/diff.go` for current inventory use cases.
- Making lint failures part of the synchronous `/gl/*` request path.
- Supporting every external ruleset feature on day one.
- Solving speculative OpenAPI 3.1 migration in this document.

## Current Baseline (Observed)
Runtime validation is already implemented with `kin-openapi`:
- `internal/http/runtime_contract.go` pins the runtime contract to `openapi3` and `openapi3filter`.
- `internal/http/runtime_spec_cache.go` loads stored `spec_json` with `openapi3.NewLoader()` and validates documents before caching.
- `internal/http/runtime_validation.go` validates incoming HTTP requests with `openapi3filter.ValidateRequest`.
- `internal/http/runtime_response.go` builds deterministic stub responses and validates them with `openapi3filter.ValidateResponse`.

OpenAPI ingestion and canonicalization already exist:
- `internal/openapi/resolver.go` resolves roots and local `$ref` closures from GitLab-backed repository files.
- `internal/openapi/inspect.go` extracts endpoint data from canonical spec JSON.

Revision diffing already exists, but at Shiva's own semantic-summary level:
- `internal/openapi/diff.go` computes added, removed, and changed endpoints plus parameter and schema fingerprints.

What does not exist in the codebase today:
- no static lint subsystem
- no normalized lint finding model
- no explicit breaking-change engine with published rule IDs and severities

Search baseline:
- there are current code references to `kin-openapi`
- there are no current code references to `vacuum`, `Spectral`, `Redocly`, `libopenapi`, or `oasdiff`

## Target Contract or Target Architecture
Shiva will treat OpenAPI correctness as three separate layers.

### 1. Runtime Validation Layer
Authority:
- `kin-openapi`

Owner:
- `internal/http`

Purpose:
- validate live `/gl/*` requests and generated responses against one resolved spec snapshot

Rules:
- runtime handlers must depend only on stored canonical specs and `kin-openapi`
- runtime behavior must not depend on lint-policy engines
- runtime validity is binary for request handling: pass or fail

### 2. Static Lint and Policy Layer
Authority:
- `vacuum`

Owner:
- new `internal/openapi/lint` package

Purpose:
- detect structural issues, quality issues, and organization policy violations on ingested specs

Rules:
- linting runs off the request path
- linting consumes deterministic spec input for a specific repo revision and API root
- findings are normalized into Shiva-owned result records
- finding records must preserve:
  - checker name
  - rule ID
  - severity
  - file path when available
  - line and column when available
  - human-readable message
  - repo and revision identity

### 3. Breaking-Change Layer
Authority:
- `oasdiff`

Owner:
- new `internal/openapi/breaking` package

Purpose:
- classify revision-to-revision compatibility changes with stable rule IDs and severities

Rules:
- breaking analysis compares two canonical specs for the same API root
- it must not compare raw repo files directly
- breaking findings are stored separately from lint findings
- Shiva's existing semantic diff remains available for lightweight inventory summaries and webhook payload shaping

### Cross-Layer Invariants
- One tool family owns one concern.
- Runtime validation remains Go-native and embedded.
- Static policy checks remain asynchronous and revision-scoped.
- Breaking analysis compares canonical artifacts, not authoring layout.
- External outputs use Shiva-owned normalized schemas, not raw third-party output blobs.

## Implementation Notes
### Package Boundaries
Add:
- `internal/openapi/lint`
- `internal/openapi/breaking`

Keep ownership:
- `internal/http` owns runtime request and response validation
- `internal/openapi` owns canonicalization, extraction, diff orchestration, and new validation orchestration

### Result Model
Introduce a shared normalized finding model for asynchronous checks.

Required fields:
- `checker`
- `kind`
  - `lint`
  - `breaking`
- `rule_id`
- `severity`
- `message`
- `spec_path`
- optional `line`
- optional `column`
- repo identity
- API root identity
- revision identity

Do not reuse runtime request-failure objects for this. Runtime failures are request-local. Lint and breaking findings are revision artifacts.

### Execution Timing
Run order after canonical artifact persistence:
1. run static lint on the canonical spec for the rebuilt API root
2. run Shiva semantic diff as today
3. run breaking-change analysis against the previous canonical spec when one exists
4. persist normalized findings
5. expose findings to webhooks, CLI, and future UI

This ordering preserves deterministic inputs and keeps runtime caching isolated from ingest-time analysis.

### Tool Integration Rules
For `kin-openapi`:
- no change to runtime package ownership
- continue validating loaded runtime documents before caching

For `vacuum`:
- prefer library integration over shelling out
- limit the first implementation to a Shiva-owned built-in ruleset plus optional imported rulesets
- treat raw third-party severities as input, then map them into Shiva severity values explicitly

For `oasdiff`:
- wrap it behind Shiva-owned interfaces
- persist normalized rule IDs, severities, and messages instead of opaque tool blobs
- compare canonical previous/current specs from storage, not repo files

### Failure Behavior
- runtime validation failures continue to affect only the active request
- lint execution failure must not silently pass as "clean"
- tool execution failure must be represented distinctly from successful validation with zero findings
- permanent spec invalidity should remain visible on the affected API revision

## Milestones
### Milestone 1
Scope:
- introduce normalized finding model and `internal/openapi/lint` boundary
- integrate `vacuum` for canonical-spec linting

Expected results:
- Shiva can produce revision-scoped lint findings for each rebuilt API root

Testable outcome:
- a rebuilt API revision with a known lint violation produces persisted findings with stable rule IDs and locations

### Milestone 2
Scope:
- introduce `internal/openapi/breaking`
- integrate `oasdiff` on previous/current canonical artifacts

Expected results:
- Shiva can distinguish semantic inventory diff from compatibility breakage

Testable outcome:
- a revision that adds a breaking request or response change produces a persisted breaking finding with checker-specific rule metadata

### Milestone 3
Scope:
- expose findings through CLI, webhook, or query surfaces

Expected results:
- validation output becomes consumable by humans and automation

Testable outcome:
- a processed revision surfaces lint and breaking findings without requiring direct database inspection

## Acceptance Criteria
- `/gl/*` runtime behavior continues to use `kin-openapi` only.
- Static linting runs outside the request path.
- Breaking analysis compares canonical stored specs, not raw repository files.
- Shiva produces normalized lint findings with stable checker, rule, severity, and message fields.
- Shiva produces normalized breaking findings that are distinct from semantic endpoint diffs.
- No Node runtime is required for service-side validation execution.

## Risks
- `kin-openapi` and `oasdiff` may diverge in interpretation on edge cases because they are separate implementations.
- `vacuum` rule coverage may not map cleanly to every organization policy without custom rules.
- OpenAPI 3.1 requirements may pressure Shiva toward a future parser migration if `kin-openapi` remains behind Shiva's needed level.
- Normalizing external findings too aggressively can hide useful checker-specific context.

## References
- [OpenAPI validation frameworks](../research/openapi-validation-frameworks.md)
- [Competitive positioning](../research/compete.md)
- [Current scope](../README.md)
- [Endpoints and runtime contract](../docs/endpoints.md)
- `internal/http/runtime_contract.go`
- `internal/http/runtime_spec_cache.go`
- `internal/http/runtime_validation.go`
- `internal/http/runtime_response.go`
- `internal/openapi/resolver.go`
- `internal/openapi/inspect.go`
- `internal/openapi/diff.go`
