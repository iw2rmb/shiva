# Repo-Backed Runtime Endpoints

Scope: Add a separate `/gl/*` runtime HTTP surface that resolves repo-backed operations from stored OpenAPI snapshots, validates incoming requests against the matched operation, and returns deterministic spec-shaped stub responses without proxying upstream traffic.

Documentation: `docs/endpoints.md`

Legend: [ ] todo, [x] done.

- [x] 1.1 Lock the runtime contract and library choice
  - Repository: `shiva`
  - Component: `docs/endpoints.md`, `internal/http`, `go.mod`
  - Verification: route contract is documented, selector grammar is explicit, framework choice is recorded in code/docs
  - Reasoning: high
1. Add a new runtime surface section to `docs/endpoints.md` for `<method> /gl/*` and keep the current `/v1/*` query transport documented as a separate concern.
2. Define the route grammar as `/gl/<repo-path>/<openapi-path>` and `/gl/<repo-path>/@<selector>/<openapi-path>`, where `<selector>` is only `latest` or an 8-character lowercase SHA in v1.
3. Define repo-path resolution as longest existing repo-prefix match over the segments after `/gl/`, then treat the next segment as optional `@selector`, and treat the remaining suffix as the OpenAPI path.
4. Choose `github.com/getkin/kin-openapi/openapi3` plus `openapi3filter` for request/response validation, and explicitly reject static middleware such as `oapi-codegen` Fiber adapters because Shiva resolves routes dynamically from stored repo specs.

- [x] 1.2 Add runtime route parsing and repo/snapshot resolution
  - Repository: `shiva`
  - Component: `internal/http/server.go`, `internal/http/runtime_route_parser.go`, `internal/store`
  - Verification: `/gl/group/repo/pets`, `/gl/group/repo/@latest/pets`, and `/gl/group/subgroup/repo/@deadbeef/pets` resolve deterministically to one repo snapshot
  - Reasoning: high
1. Register all supported OpenAPI HTTP methods on `/gl/*` in `internal/http/server.go` and route them to one runtime handler instead of extending `/v1/*`.
2. Implement a parser in `internal/http` that splits the path after `/gl/`, generates repo-prefix candidates longest-first, resolves the first existing `(namespace, repo)` pair, extracts optional `@selector`, and normalizes the remaining suffix to a canonical OpenAPI path with a leading `/`.
3. Add a store helper that can answer repo existence or repo lookup by `(namespace, repo)` for the runtime parser without listing the full repo catalog on every request.
4. Map selector inputs to the existing snapshot-resolution model: no selector and `@latest` use default-branch latest, `@<sha8>` uses SHA resolution, unsupported selectors return `400`.

- [x] 1.3 Resolve runtime operations and cache parsed OpenAPI documents
  - Repository: `shiva`
  - Component: `internal/http/runtime_route.go`, `internal/http/runtime_spec_cache.go`, `internal/store`
  - Verification: a runtime request resolves one operation, duplicate cross-API matches return candidates, repeated calls reuse parsed spec state for the same `api_spec_revision_id`
  - Reasoning: high
1. Reuse `store.ResolveOperationCandidatesByMethodPath` to resolve the request method and normalized path against the selected repo snapshot, without adding a second operation index.
2. Return `404` when no operation matches and `409` with candidate rows when multiple APIs define the same `(method, path)` inside the same repo snapshot.
3. Load the matched API artifact through `GetSpecArtifactByAPISpecRevisionID`, parse `SpecJSON` into `openapi3.T`, and store the parsed document in an immutable cache keyed by `api_spec_revision_id`.
4. Keep the cache read-only per revision id so there is no invalidation protocol beyond selecting a different revision id on the next request.

- [x] 1.4 Validate requests and build spec-shaped stub responses
  - Repository: `shiva`
  - Component: `internal/http/runtime_validation.go`, `internal/http/runtime_response.go`, `internal/http/query_helpers.go`
  - Verification: invalid query/header/body inputs are rejected by the matched operation schema, documented error responses are preferred, valid requests return response bodies that also pass response validation
  - Reasoning: xhigh
1. Convert the Fiber request into an `http.Request` plus path parameters and call `openapi3filter.ValidateRequest` for query params, headers, path params, body shape, and content-type checks against the matched operation.
2. Add a narrow `AuthenticationFunc` for OpenAPI security schemes that validates required credential presence and location for API key, bearer, basic, query, and cookie inputs without performing external credential verification.
3. Select validation-error responses deterministically: prefer a documented status for the failure class (`401`, `403`, `404`, `406`, `415`, `422`, then `400`), then `default` if present, then fallback to `400` JSON with an explanation when the spec does not describe an error response.
4. Select success responses deterministically by the lowest explicit `2xx` status, render the body from `example`, first `examples` entry, or a minimal schema-generated payload, and run `openapi3filter.ValidateResponse` before writing the final response.

- [ ] 1.5 Cover the runtime contract with tests and shipped docs
  - Repository: `shiva`
  - Component: `internal/http`, `docs/endpoints.md`, `docs/testing.md`, `README.md`
  - Verification: focused runtime endpoint tests pass, full `go test ./...` passes, docs link check passes, shipped docs describe both `/v1/*` query transport and `/gl/*` runtime transport
  - Reasoning: high
1. Add table-driven tests for longest repo-prefix parsing, selector parsing, snapshot resolution, operation ambiguity, and method/path normalization on the new `/gl/*` surface.
2. Add request-validation and response-generation tests that cover required headers, invalid query types, invalid JSON bodies, undocumented validation errors falling back to `400`, documented error responses, and valid JSON stub responses for single-API and multi-API repos.
3. Update `docs/endpoints.md`, `docs/testing.md`, and `README.md` to describe the new runtime surface, its non-proxy behavior, and the fact that the current `/v1/call` endpoint remains planning-only.
4. Run `go test ./internal/http ./internal/store`, then `go test ./...`, then `~/@iw2rmb/auto/scripts/check_docs_links.sh`.
