# Release Readiness and Test Suite (Item 12)

## Status
- Implemented: item-12 test coverage expansion and release-readiness deliverables.
- Scope completed:
  - unit tests for selector detection, semantic diff, and OpenAPI resolution edge cases,
  - deterministic integration-style test for webhook->worker->build->diff->notify,
  - fixture-based split-file OpenAPI E2E path,
  - operational runbook and minimal deployment manifest.

## Test Coverage Additions
- Selector detection/unit input normalization:
  - `internal/store/read_selector_test.go`
  - expanded `TestNormalizeResolveReadSelectorInput` with invalid/edge selector modes.
- Semantic diff unit coverage:
  - `internal/openapi/diff_test.go`
  - expanded `TestComputeSemanticDiff` for removed-endpoint accounting and invalid payload rejection.
- OpenAPI resolver unit coverage:
  - `internal/openapi/resolver_test.go`
  - added deleted-candidate detection, non-match behavior, and invalid `$ref` coverage.
- Integration webhook-to-notify flow:
  - `cmd/shiva/webhook_to_notify_integration_test.go`
  - deterministic fake queue/store/GitLab + real `revisionProcessor`, `worker.Manager`, resolver, and notifier.
- Fixture-based split-file OpenAPI E2E:
  - `internal/openapi/split_file_e2e_test.go`
  - fixtures under `internal/openapi/testdata/fixtures/split-file/`.

## Release Readiness Artifacts
- Operations runbook:
  - `docs/operations-runbook.md`
- Minimal Kubernetes deployment manifest:
  - `deploy/k8s/shiva.yaml`

## CI/Validation Targets
- Focused verification:
  - `go test ./cmd/shiva -run TestIntegrationWebhookToNotifyFlow`
  - `go test ./internal/openapi -run 'TestResolverResolveChangedOpenAPI|TestComputeSemanticDiff|TestSplitFileFixtureE2E'`
  - `go test ./internal/store -run TestNormalizeResolveReadSelectorInput`
- Full suite:
  - `go test ./...`

## References
- Runtime baseline: `docs/runtime-baseline.md`
- Webhook ingest: `docs/gitlab-webhook-ingest.md`
- Worker processing: `docs/ingest-worker-processing.md`
- OpenAPI resolution: `docs/openapi-candidate-resolution.md`
- Canonical build and persistence: `docs/canonical-spec-build-persistence.md`
- Semantic diff: `docs/semantic-diff-engine.md`
- Outbound notifications: `docs/outbound-webhook-notifications.md`
- Hardening controls: `docs/hardening-observability-security-controls.md`
- Roadmap: `roadmap/shiva.md`
