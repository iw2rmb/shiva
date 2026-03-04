# Incremental OpenAPI Update Design

## Scope
This design defines update behavior after API instances are already known for a repository. Initial full-repo ingestion is defined in `design/init.md`. Monorepo entities and read contracts are defined in `design/mono.md`.

The design assumes no backward compatibility constraints.

## Phase Gate
- This design is the next step after full ingestion bootstrap from `design/init.md` is implemented and verified.
- It requires monorepo persistence/entities from `design/mono.md` (`api_specs`, `api_spec_revisions`, `api_spec_dependencies`).
- Until that gate is complete, current delta-only incremental behavior remains expected.

## Input
- Changed paths from GitLab compare: `parent_sha -> sha`.

## Preconditions
- Active API instances exist for repository.
- If no API instances exist, run bootstrap flow from `design/init.md`.

## Impact Resolution
For each active API instance:
- Load latest dependency set for that API instance.
- Mark API instance as impacted if any changed path intersects:
  - Root path.
  - Any dependency path in its API graph.

## Build Rules
- Rebuild only impacted API instances.
- Carry non-impacted API instances forward logically without rebuild.
- If root file is deleted, deactivate API instance and emit removal diff/event for that API.

## Fallback Discovery Rule
If changed paths include create/rename events matching discovery heuristics and no impacted APIs were found:
- Run targeted discovery on changed files.
- Create new API instances for any newly valid roots.

Discovery heuristics are the same candidate filtering pipeline from `design/init.md`:
- path normalization,
- ignore filtering,
- extension prefilter,
- top-level sniff,
- parse + top-level `openapi`/`swagger` validation.

## Failure Handling
- Invalid root or invalid local `$ref` fails only that API instance revision.
- Other impacted API instances in same repository revision continue processing.
- Repo-level infrastructure failures (GitLab auth/network, DB outage) fail whole revision job.

## Performance Constraints
- Avoid full-tree scans in normal incremental mode.
- Resolve impact via changed-path intersection against stored dependency sets.
- Use targeted discovery only as fallback for potential new roots.

## Tests
- Dependency intersection triggers rebuild of impacted API only.
- Unrelated path changes do not trigger rebuild.
- Newly added root in changed files is discovered via fallback rule.
- Deleted root deactivates API instance and emits removal diff/event.
- One API failure does not block processing of another impacted API instance.

## References
- Initial full-repo ingestion: `design/init.md`
- Monorepo data model and read contract: `design/mono.md`
- GitLab compare ingestion behavior: `docs/gitlab.md`
