# OpenAPI Validation Frameworks

## Scope
This note compares frameworks Shiva could use to check OpenAPI correctness, lint specs, and detect revision-level contract breakage.

Related documents:
- [Shiva competitive positioning](./compete.md)
- [OpenAPI validation architecture](../design/openapi-validation-architecture.md)
- [Runtime and query endpoints](../docs/endpoints.md)

Research date:
- 2026-03-12

## Evaluation Criteria
Shiva needs three distinct capabilities:
- embedded Go validation for request and response correctness at runtime
- static linting and policy enforcement during ingest and review
- breaking-change detection between revisions

Secondary constraints:
- self-hosted by default
- Go-first implementation
- support for multi-file specs
- line-aware diagnostics when possible
- clear ownership boundaries between runtime validation and lint policy

## Frameworks

### kin-openapi
Primary source:
- <https://github.com/getkin/kin-openapi>

What it is:
- a Go library for OpenAPI parsing and validation
- includes `openapi3` document handling and `openapi3filter` request and response validation

Fit for Shiva:
- strongest fit for embedded runtime request and response validation
- already in Shiva's dependency graph and runtime code

Pros:
- native Go library
- already used by Shiva runtime
- supports request and response validation directly
- supports loading and validating OpenAPI documents in-process

Cons:
- OpenAPI 3.1 support is still marked as upcoming in the project README
- not a full lint and policy engine by itself

Recommendation:
- keep as Shiva's runtime validation authority

### vacuum
Primary sources:
- <https://quobix.com/vacuum/about/>
- <https://quobix.com/vacuum/api/getting-started/>
- <https://github.com/daveshanley/vacuum>

What it is:
- a Go-based OpenAPI linter and quality checker
- compatible with Spectral rulesets and reports

Fit for Shiva:
- strongest fit for static spec linting inside a Go-first system

Pros:
- Go-native
- designed as both CLI and library
- explicitly focused on OpenAPI linting and quality checks
- compatible with Spectral rulesets, which reduces policy lock-in
- positioned for large-spec performance

Cons:
- smaller ecosystem than Spectral
- policy authors may still think in Spectral terms, so rule ownership must stay disciplined

Recommendation:
- use as the primary lint and policy engine

### Spectral
Primary source:
- <https://github.com/stoplightio/spectral>

What it is:
- a flexible JSON/YAML linter with built-in OpenAPI support and custom rulesets

Fit for Shiva:
- strong ruleset ecosystem
- weaker fit for an embedded Go service because it adds a Node toolchain boundary

Pros:
- mature policy ecosystem
- custom rulesets and functions
- broad OpenAPI support

Cons:
- Node-based
- weaker operational fit for Shiva's Go runtime and worker model
- better as an external ecosystem compatibility target than as Shiva's embedded authority

Recommendation:
- treat as compatibility input, not as Shiva's primary implementation choice

### Redocly CLI
Primary sources:
- <https://redocly.com/docs/cli/commands/lint>
- <https://redocly.com/redocly-cli/>

What it is:
- an OpenAPI multi-tool with linting, validation, and bundling

Fit for Shiva:
- useful reference for CLI UX and lint-report ergonomics
- not the best embedded engine choice for a Go service

Pros:
- strong packaged CLI experience
- built-in rulesets and ignore-file support
- good multi-file workflow

Cons:
- Node-based
- broader product than Shiva needs for embedded validation
- less suitable than Go-native tooling for service-side ownership

Recommendation:
- not preferred for Shiva internals

### libopenapi and libopenapi-validator
Primary sources:
- <https://pb33f.io/libopenapi/>
- <https://pb33f.io/libopenapi/validation/>

What it is:
- a Go OpenAPI library with high/low-level models, indexing, diff features, and an optional validator module

Fit for Shiva:
- good candidate when Shiva needs deeper static analysis, indexing, and source-location-aware inspection
- weaker immediate fit as a second runtime validator alongside `kin-openapi`

Pros:
- Go-native
- full OpenAPI 3.1 support is explicitly claimed
- separate validator module for document, request, and response validation
- strong indexing and model-inspection story

Cons:
- overlaps with Shiva's existing `kin-openapi` runtime stack
- adopting it for runtime validation would create two authorities unless Shiva migrates deliberately
- larger integration move than the current need requires

Recommendation:
- do not introduce it into the runtime path now
- keep it as the main fallback if Shiva later needs 3.1-first validation or deeper static indexing than `vacuum` provides

### oasdiff
Primary sources:
- <https://www.oasdiff.com/>
- <https://github.com/oasdiff/oasdiff>

What it is:
- a dedicated OpenAPI diff and breaking-change detector

Fit for Shiva:
- strongest fit for revision-to-revision contract breakage
- complements, rather than replaces, document validation and linting

Pros:
- purpose-built for breaking changes
- explicit breaking-change taxonomy
- supports multi-file specs
- supports OpenAPI 3.0 and 3.1 beta

Cons:
- not a request/response validator
- not a general lint engine

Recommendation:
- use as the breaking-change layer, separate from runtime validation and static linting

## Recommended Stack
Recommended division of responsibility:
- runtime request and response validation: `kin-openapi`
- static linting and policy enforcement: `vacuum`
- revision-to-revision breaking-change analysis: `oasdiff`

Fallback or future option:
- `libopenapi` if Shiva later needs a different static-analysis core or stronger OpenAPI 3.1-first behavior inside Go

Not recommended as primary embedded engines:
- `Spectral`
- `Redocly CLI`

## Why This Split Fits Shiva
This split follows Shiva's actual shape:
- the runtime already uses `kin-openapi` in `internal/http`
- linting belongs in the ingest and review pipeline, not in the runtime request path
- breaking-change detection is revision-to-revision analysis, not document correctness

Using one tool for all three concerns would blur boundaries and create more coupling than value.

## Bottom Line
Shiva should not look for one universal OpenAPI correctness framework.

It should use:
- `kin-openapi` for runtime truth
- `vacuum` for lint and policy checks
- `oasdiff` for compatibility breakage

That keeps each concern owned by the tool family that is best aligned with Shiva's architecture.
