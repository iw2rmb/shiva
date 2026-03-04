# Initial OpenAPI Ingestion Design

## Scope
This design defines first-time ingestion when a repository has no known API instances yet. Monorepo storage and contracts are defined in `design/mono.md`. Incremental updates are defined in `design/inc.md`.

The design assumes no backward compatibility constraints.

## Problem
A delta-only resolver (`compare(parent_sha, sha)` + changed-path filtering) misses existing OpenAPI roots when the first processed revision does not touch spec files.

## Goals
- Discover all valid OpenAPI roots at a target `sha` even when webhook diff is unrelated.
- Build and persist API instances from discovered roots.
- Apply ignore rules to avoid scanning generated and test noise paths.

## Non-Goals
- Supporting remote `$ref` targets.
- Supporting multiple Git providers in this iteration.

## Trigger
Bootstrap discovery runs when either condition is true:
- Repo has zero active API instances.
- Forced rescan flag is set for the repo.

## Source of Files
Add GitLab tree listing in client surface:
- `ListRepositoryTree(ctx, projectID, sha, path, recursive)` with pagination.

Discovery uses full repository tree at `sha` and does not depend on compare diff output.

## Candidate Filtering Pipeline
1. Normalize path.
2. Apply ignore filters:
   - Built-in defaults.
   - `.shivaignore` patterns from repository root at same `sha` when present.
3. Extension prefilter (`.yaml`, `.yml`, `.json`).
4. Fast content sniff:
   - YAML: top-level `openapi:` or `swagger:`.
   - JSON: top-level `"openapi"` or `"swagger"`.
5. Strict parse and validate with resolver parser.

Only candidates passing step 5 become API roots.

## `.shivaignore` Specification

### Location
- Optional file at repository root: `.shivaignore`.

### Syntax
- One pattern per line.
- `#` starts a comment.
- Empty lines are ignored.
- Patterns use doublestar semantics.
- Leading `/` anchors to repo root.
- Negation (`!`) is not supported in v1.

### Effective Ignore Set
`effective_ignores = built_in_ignores + file_ignores`

Built-in defaults:
- `**/test*/**`
- `**/__tests__/**`
- `**/node_modules/**`
- `**/vendor/**`

## Bootstrap Result
For each discovered root:
- Resolve local `$ref` closure.
- Build canonical artifact.
- Upsert API instance.
- Persist per-API revision artifact and endpoint index.

Revision status:
- Mark processed with `openapi_changed=true` if at least one API instance was built.
- Mark processed with `openapi_changed=false` if zero roots were discovered.

## Performance Constraints
- Use paginated tree listing.
- Use bounded concurrency for content reads.
- Use prefix/sniff reads before full parse where possible.

## Tests
- Unrelated first webhook still discovers pre-existing OpenAPI roots.
- `.shivaignore` excludes test fixtures and generated paths.
- Malformed `.shivaignore` lines are handled deterministically.
- Zero-discovery bootstrap still marks revision processed with `openapi_changed=false`.

## Open Questions
- Should `.shivaignore` support negation (`!pattern`) in v1?

## References
- Monorepo entities and contracts: `design/mono.md`
- Incremental update flow: `design/inc.md`
- GitLab ingestion behavior: `docs/gitlab.md`
