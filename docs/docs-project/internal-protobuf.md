# Internal Protobuf Contracts

Gateyes keeps internal control, inference runtime, and workflow RPC contracts
under `proto/*/v1`. Generated Go bindings under `pkg/*/v1` are committed so
consumers can compile without installing Protobuf generators.

## Compatibility Rules

- Never reuse a committed field number.
- Reserve deleted field names and numbers.
- Keep an `*_UNSPECIFIED = 0` value in every enum.
- Carry schema version, idempotency key, and request/trace metadata explicitly.
- Keep control, runtime, and workflow error codes in their own namespaces.
- Do not translate these contracts into changes to public HTTP, JSON, SSE,
  errors, or headers.

Use `make proto` after editing a proto. `make proto-check` regenerates bindings
and fails on drift. `make proto-lint` applies Buf STANDARD lint rules, and
`make proto-breaking` compares the schema with the `main` branch using Buf FILE
compatibility rules.

## Tool And Dependency Record

| Component | Pinned version | License | Maintenance evidence |
| --- | --- | --- | --- |
| Buf CLI | 1.72.0 | Apache-2.0 | Upstream release published 2026-07-17 |
| protoc-gen-go | 1.36.11 | BSD-3-Clause | Upstream release published 2025-12-12; runtime already used by the project |
| protoc-gen-go-grpc | 1.6.2 | Apache-2.0 | Upstream release published 2026-05-11 |
| google.golang.org/protobuf runtime | existing go.mod pin | BSD-3-Clause | Already a direct project dependency |
| google.golang.org/grpc runtime | existing go.mod pin | Apache-2.0 | Already a direct project dependency |

The generator plugins are pinned by immutable version labels in
`buf.gen.yaml`; the Buf CLI is installed at its exact Go module version into a
repository-local ignored directory. No new runtime dependency is introduced.

## Vulnerability Check

On 2026-09-05, public GitHub issue searches for open CVE reports in
`bufbuild/buf`, `protocolbuffers/protobuf-go`, and `grpc/grpc-go` returned zero
results for the pinned generators. GitHub Dependabot alert APIs for those
upstream repositories are not public to this project, so that signal could not
be inspected.

`govulncheck` 1.7.0 reported 15 reachable vulnerabilities in the repository's
existing Go 1.26.3 standard library and existing runtime modules. These include
GO-2026-6061 in `google.golang.org/grpc` 1.80.0 (fixed in 1.82.1), along with
findings in the standard library, `golang.org/x/net`, `golang.org/x/text`, and
OpenTelemetry. Every reported call trace starts in pre-existing application or
plugin code; none starts in the new generated packages, and this change adds no
Go module dependency. Upgrading those shared runtime dependencies is therefore
separate from this contract-only task. This is a point-in-time check, not a
guarantee that the tools are vulnerability-free.
