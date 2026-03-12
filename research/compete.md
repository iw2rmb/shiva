# Shiva Competitive Positioning

## Scope
This note focuses on how Shiva should compete in the current API tooling market.

Related notes:
- [Alternative tools comparison](./compare.md)
- [Current product scope](../README.md)
- [Endpoint and runtime contract](../docs/endpoints.md)
- [CLI surface](../docs/cli.md)

Research date:
- 2026-03-12

## Shiva Today
Shiva already has a differentiated foundation:
- GitLab push-event ingestion
- canonical OpenAPI build and indexing
- semantic diff computation
- repo, API, and operation inventory endpoints
- commit-addressable snapshot selection
- repo-backed runtime validation and deterministic stub responses
- CLI inspect, inventory, sync, and call-planning flows

That means Shiva already competes in runtime mocking, but from a different angle than generic mock servers.

## Market Shape
The market splits into two adjacent categories.

Mock-first products:
- Prism
- Microcks
- Mockoon
- WireMock
- MockServer

Governance and change-management products:
- Bump.sh
- Optic
- oasdiff

Inference:
- plain OpenAPI mock serving is crowded
- Git-native API governance and runtime from exact repo snapshots is less crowded
- Shiva should enter through that second seam, then add runtime mocking on top

## Recommended Positioning
Primary positioning:
- self-hosted, GitLab-native API source of truth

Positioning statement:
- Shiva ingests OpenAPI from GitLab repos, tracks API behavior per revision, exposes fleet-wide inspect and inventory APIs, and serves deterministic runtime behavior from exact repository snapshots.

Core wedge:
- GitLab-first instead of generic Git
- self-hosted by default
- commit-addressable runtime and inspection
- multi-repo catalog, not single-spec mocking
- diff, query, inventory, validation, and runtime stubs in one system

## Ideal Customer Profile
Best-fit teams:
- platform teams in GitLab-heavy organizations
- internal developer platform groups
- regulated or enterprise teams that cannot rely on SaaS API governance
- teams with many repo-local OpenAPI specs and weak API discoverability
- teams that need reproducible API behavior by commit or branch state

Poor-fit teams:
- single-service teams that just need a local mock server
- frontend-only teams looking for a desktop mock editor
- teams optimizing for broad protocol coverage over OpenAPI depth
- buyers that prefer hosted documentation portals over self-hosted control

## Anti-Goals
Shiva should not lead with:
- desktop mock design UX
- AI-generated mocks as the core value proposition
- multi-protocol breadth before OpenAPI depth
- generic single-file mock serving
- polished docs portal as the primary product story

Those areas are already stronger for existing competitors and do not use Shiva's current architecture well.

## Competitor Matrix
| Competitor | Strongest area | Where Shiva can win | Where Shiva is weak today |
|---|---|---|---|
| Prism | lightweight OpenAPI mocking and validation CLI | GitLab ingestion, snapshot-aware queries, catalog across repos, self-hosted control plane | Prism still has a simpler local-first developer story |
| Microcks | rich mocking, contract testing, multi-protocol platform | simpler architecture, narrower OpenAPI-first story, easier adoption for internal platform teams | Microcks is much stronger on test automation, examples, and stateful mocks |
| WireMock Cloud | enterprise simulation, Git sync, managed workflow | self-hosted deployment, GitLab-native workflow, lower platform weight | WireMock has stronger simulation UX, Git productization, and enterprise polish |
| Mockoon | local-first mock authoring UX | repo-native governance and inspect story | Mockoon is better for users who want manual mock editing and desktop workflows |
| Bump.sh | hosted docs and API change management | self-hosted runtime, exact snapshot behavior, integrated inspect/call surface | Bump is stronger for hosted review and documentation workflows |
| Optic | API diff and standards enforcement | runtime plus governance in one system, GitLab event-driven ingestion | Optic is stronger for mature review-time change controls today |
| oasdiff | diff engine and breaking-change analysis | broader product surface, runtime/catalog integration | oasdiff is a sharper dedicated diff tool right now |

## Real Competitive Wedge
Shiva should compete on this claim:

- one system for ingesting, diffing, cataloging, inspecting, and running OpenAPI-defined behavior from exact Git snapshots

That wedge is materially different from:
- "mock this spec locally"
- "show a docs portal"
- "lint a diff in CI"

Those are features. Shiva's stronger story is repository-native API state management.

## Expansion Areas
To compete beyond the current wedge, Shiva should add adjacent capabilities that reinforce the existing runtime and governance model.

High-value additions:
- scenario and call-series tracking tied to test runs, CI jobs, or named flows
- spec linting and policy enforcement for mismatches, org rules, and breaking changes
- external OpenAPI uploads for teams that cannot integrate GitLab access immediately
- richer webhook events for lint failures, diff severity, and coverage changes
- better human UX on top of revision and branch selection

Potential traps:
- generic uptime monitoring against `servers` URLs is a separate market and should stay narrowly scoped to contract reachability or drift checks
- a TUI can help adoption, but it is not a primary differentiator

## Strategic Sequence
Recommended order:

1. Add spec linting and policy enforcement on top of the existing diff pipeline.
2. Add external OpenAPI onboarding so teams can adopt Shiva before GitLab integration.
3. Add scenario and call-series tracking tied to runtime traffic and tests.
4. Expand webhook events around diffs, lint results, and scenario coverage.
5. Add stronger catalog UX on top of the existing query endpoints and CLI.
6. Add a TUI only after the product model is stable.

This order uses Shiva's current strengths instead of rebuilding capabilities that the runtime surface already has.

## Messaging
Good message:
- GitLab-native API source of truth
- inspect and run APIs exactly as of commit `abc12345`
- self-hosted API runtime and governance for internal platforms
- deterministic spec-backed stubs from exact repository snapshots

Weak message:
- another OpenAPI mock server
- AI mocks for developers
- Postman replacement

## Bottom Line
Shiva should compete in the overlap of:
- API governance
- repository-native API cataloging
- snapshot-aware runtime mocking
- test and scenario intelligence

It should not enter as a generic mock server.

That gives Shiva a defensible story against Prism, Microcks, WireMock, Bump.sh, and Optic:
- not broader than all of them
- narrower, but sharper and more opinionated around GitLab, self-hosting, and exact revision behavior

## Sources
- APIsprout: <https://github.com/danielgtaylor/apisprout>
- Prism: <https://github.com/stoplightio/prism>
- Microcks: <https://microcks.io/>
- Microcks releases: <https://github.com/microcks/microcks/releases>
- Microcks stateful mocks: <https://microcks.io/documentation/guides/usage/stateful-mocks/>
- Mockoon releases: <https://github.com/mockoon/mockoon/releases>
- Mockoon OpenAPI compatibility: <https://mockoon.com/docs/latest/openapi/openapi-specification-compatibility/>
- WireMock Cloud: <https://www.wiremock.io/>
- WireMock OpenAPI Git integration: <https://docs.wiremock.io/openAPI/openapi-git-integration>
- Bump.sh API change management: <https://bump.sh/api-change-management/>
- Bump.sh GitLab CI: <https://docs.bump.sh/help/continuous-integration/gitlab-ci/>
- Optic capture and review docs: <https://github.com/opticdev/optic/wiki/Using-Optic-Capture-with-Integration-Tests>
- oasdiff: <https://github.com/oasdiff/oasdiff>
