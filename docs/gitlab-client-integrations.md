# GitLab Client Integrations (Item 5)

## Status
- Implemented: GitLab API client integrations for compare changed paths and repository file content fetch by `ref=sha`.
- Scope completed:
  - Compare endpoint client for changed paths.
  - Repository files endpoint client for file content at target revision SHA.
  - No archive download or repository clone usage.
- Current usage:
  - item 6 OpenAPI candidate detection and recursive `$ref` resolution uses this client surface directly.

## Package
- `internal/gitlab/client.go`

## Implemented Client Surface
- `NewClient(baseURL, token, ...options)`:
  - Normalizes base URL to GitLab API v4 path (`/api/v4`).
  - Uses Go standard library `net/http` client by default.
- `CompareChangedPaths(ctx, projectID, fromSHA, toSHA)`:
  - Calls `GET /projects/:id/repository/compare?from=<fromSHA>&to=<toSHA>`.
  - Returns changed path metadata (`old_path`, `new_path`, `new_file`, `renamed_file`, `deleted_file`).
- `GetFileContent(ctx, projectID, filePath, ref)`:
  - Calls `GET /projects/:id/repository/files/:file_path?ref=<sha>`.
  - Decodes `base64` content payload and returns raw file bytes.

## Error Model
- `ErrNotFound` for `404` responses.
- `APIError` for non-2xx GitLab responses other than `404`, including status and body snippet.

## Tests
- `internal/gitlab/client_test.go`
  - `TestClientCompareChangedPaths`
  - `TestClientCompareChangedPathsErrors`
  - `TestClientGetFileContent`
  - `TestClientGetFileContentErrors`

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Webhook ingest: `docs/gitlab-webhook-ingest.md`
- Worker processing: `docs/ingest-worker-processing.md`
- OpenAPI resolution flow: `docs/openapi-candidate-resolution.md`
- Design: `design/shiva.md`
- Roadmap: `roadmap/shiva.md`
