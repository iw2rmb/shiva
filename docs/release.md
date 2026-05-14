# Release

## Scope
This document defines the current Shiva binary release contract.

## Version Source
- Repository version is tracked in the root `VERSION` file.
- Tags use `v`-prefixed semantic versions (example: `v0.1.0`).

## Release Automation
- Workflow: `.github/workflows/release.yml`.
- Trigger: Git tag push matching `v*` (manual dispatch is also available).
- Steps:
  - `go test ./...`
  - GoReleaser `release --clean`

## GoReleaser Contract
- Config file: `.goreleaser.yaml`.
- Build targets:
  - binaries: `shiva`, `shivad`
  - OS: `linux`, `darwin`
  - arch: `amd64`, `arm64`
- Release overwrite behavior:
  - `release.mode: replace`
  - `release.replace_existing_artifacts: true`
- Checksum artifact: `checksums.txt`.
