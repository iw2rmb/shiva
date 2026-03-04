# Shiva

Shiva is a Go service that tracks OpenAPI changes in GitLab repositories, rebuilds canonical specs, and distributes updates to subscribers. It also serves tenant-scoped, versioned routes for full specs and endpoint-level views.

## Executive Summary
- Input: GitLab push webhooks.
- Core action: detect OpenAPI changes, rebuild full spec at target SHA, compute diff vs previous revision.
- Output: notify registered webhooks and expose read APIs for `sha`, `branch`, and `latest`.
- Stack: Fiber (HTTP), PostgreSQL with sqlc + pgx (storage), worker pipeline (async processing).

## Key Takeaways
- Architecture prioritizes correctness over partial patching by always rebuilding canonical artifacts when OpenAPI inputs change.
- Retrieval is targeted: GitLab Repository Files API fetches only changed/required OpenAPI files, not whole-repo snapshots.
- Per-repo ordered processing and idempotency keys prevent duplicate or out-of-order state.
- Tenant isolation is enforced in both write and read paths.
- Outbound webhooks are signed and retried with backoff to provide reliable downstream integration.
- Read APIs are selector-based (`sha|branch|latest`) for stable integration contracts.

## Primary Use Cases
- Keep consumers synchronized with API contract changes in near real time.
- Serve immutable historical specs by commit SHA.
- Serve branch-latest and repo-latest specs for environments that track moving targets.
- Provide endpoint-level lookup for gateway/tooling automation.

## Core Flows
1. Receive GitLab webhook and validate authenticity.
2. Resolve changed files in commit range.
3. If OpenAPI changed:
   - fetch changed and `$ref`-required OpenAPI files via GitLab Repository Files API at revision SHA,
   - build canonical JSON/YAML specs,
   - index endpoints,
   - compute semantic diff.
4. Notify subscribers with full and/or diff payload.
5. Serve artifacts and endpoint views via public routes.

## Planned Routes
- `POST /internal/webhooks/gitlab`
- `GET /{tenant}/{repo}/{selector}/spec.json`
- `GET /{tenant}/{repo}/{selector}/spec.yaml`
- `GET /{tenant}/{repo}/{selector}/endpoints`
- `GET /{tenant}/{repo}/{selector}/endpoints/{method}/{path}`
- `GET /{tenant}/{repo}/endpoints` (latest processed revision on `main`)

`selector` is one of:
- commit SHA
- branch name
- `latest` (default branch latest processed revision)

## Repository Status
This repository currently contains architecture/design artifacts:
- [Design Doc](design/shiva.md)
- `.gitignore`
- `README.md`

Implementation code and migrations can be added next following the design milestones in `design/shiva.md`.
