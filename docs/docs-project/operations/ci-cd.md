# CI/CD

## 1. CI

Workflow file:

```text
.github/workflows/ci.yml
```

Current checks:

1. `gofmt` (advisory warning)
2. `go test ./...`
3. `go test -race ./...`
4. `go vet ./...`
5. `govulncheck`
6. `gitleaks`
7. `trivy` filesystem scan
8. migration smoke test via `cmd/migrate`
9. architecture dependency check via `make lint-arch`
10. code quality scan via `make lint-quality`
11. test coverage report (`go test -coverprofile`)

Steps 5–8 run as advisory (`continue-on-error: true`) so the workflow remains green while still surfacing findings. Steps 9–11 run in separate jobs so they can execute in parallel with the main validation matrix.

## 2. Release

Workflow file:

```text
.github/workflows/release.yml
```

Current release path:

1. trigger on `v*` tag
2. build multi-arch Docker image
3. push image to GHCR
4. emit SBOM/provenance
5. sign image with Cosign keyless flow
6. publish GitHub release with auto notes

## 3. Live provider matrix

Workflow file:

```text
.github/workflows/live-provider.yml
```

Characteristics:

1. manual only
2. env-gated through GitHub secrets
3. runs real provider compatibility tests
