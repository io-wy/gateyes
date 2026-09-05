# Internal Protobuf Design

## Scope

Define versioned internal RPC contracts for the control plane, inference runtime,
and workflow executor. This change generates Go bindings and adds reproducible
lint, generation-drift, and breaking-change gates. It does not start servers,
add service discovery, or change public HTTP, JSON, SSE, error, or header
contracts.

## Decision

Use three independent `v1` Protobuf packages. Each RPC carries an explicit
schema version and request/trace metadata. Execution requests carry an explicit
idempotency key. Payloads stay as bytes with a content type so the internal
transport can carry the existing external request/response representation
without making the control plane part of the inference hot path.

Control, runtime, and workflow errors use separate enums and detail messages.
Runtime errors additionally identify provider, internal RPC, and configuration
domains. Workflow status remains a business-state enum; a workflow failure may
embed a runtime error while retaining its own workflow error code.

Buf supplies descriptor-aware lint and FILE-level compatibility checks against
`main`. Remote generator revisions and the Buf CLI version are pinned. Checked-in
bindings make downstream Go builds independent of generator availability.

## Alternatives

1. A shared common proto would reduce duplicate metadata fields, but it creates
   cross-boundary coupling before a genuinely shared lifecycle exists.
2. Local `protoc` plugins are familiar, but host-installed binary versions make
   generation drift harder to reproduce in CI and developer environments.
3. Hand-maintained Go RPC types avoid code generation, but lose wire-level field
   compatibility checks and violate the requirement for versioned Protobuf.

## Compatibility And Testing

Descriptor tests pin required services, streaming shape, field names/numbers,
enum zero values, and distinct error namespaces. Buf rejects incompatible
changes against `main`; deleted fields must be reserved before that gate passes.
CI regenerates bindings and fails on a dirty diff, then runs lint and the
breaking check.
