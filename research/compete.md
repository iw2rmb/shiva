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
- CLI inspect, inventory, sync, and call-planning flows

The main missing piece is also clear in the current docs:
- `/gl/*` route parsing and operation resolution exist
- request validation and spec-shaped stub responses are not implemented yet

That means Shiva is closer to an API source-of-truth platform than to a finished mock server.

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
- diff, query, inventory, and runtime in one system

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
| Prism | lightweight OpenAPI mocking and validation CLI | GitLab ingestion, snapshot-aware queries, catalog across repos, self-hosted control plane | Prism already ships working mocks and validation |
| Microcks | rich mocking, contract testing, multi-protocol platform | simpler architecture, narrower OpenAPI-first story, easier adoption for internal platform teams | Microcks is much stronger on test automation, examples, and stateful mocks |
| WireMock Cloud | enterprise simulation, Git sync, managed workflow | self-hosted deployment, GitLab-native workflow, lower platform weight | WireMock has stronger simulation features and enterprise polish |
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

## Minimum Product To Be Credible
To compete beyond positioning, Shiva needs the runtime surface to become real.

Critical gaps:
- request validation on `/gl/*`
- deterministic stub generation from examples, defaults, and schemas
- explicit no-example behavior rules
- response validation
- scenario selection when multiple examples exist
- error and failure-path modeling
- revision and branch UX for humans, not only raw API selectors

Without those pieces, Shiva is credible as API plumbing, but not yet as a direct alternative to Prism, Microcks, or WireMock.

## Strategic Sequence
Recommended order:

1. Finish `/gl/*` request validation and deterministic stub responses.
2. Add example and scenario controls so runtime behavior is usable in tests.
3. Add merge request and review workflow integration around spec diffs.
4. Add stronger catalog UX on top of the existing query endpoints and CLI.
5. Add premium or enterprise capabilities only after the core runtime is solid.

This order uses Shiva's existing strengths instead of chasing feature parity with mock-first tools.

## Messaging
Good message:
- GitLab-native API source of truth
- inspect and run APIs exactly as of commit `abc12345`
- self-hosted API runtime and governance for internal platforms

Weak message:
- another OpenAPI mock server
- AI mocks for developers
- Postman replacement

## Bottom Line
Shiva should compete in the overlap of:
- API governance
- repository-native API cataloging
- snapshot-aware runtime mocking

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
