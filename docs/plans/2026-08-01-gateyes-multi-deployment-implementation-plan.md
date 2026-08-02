# Gateyes Multi-Deployment Implementation Plan

> For agentic workers: keep the original full scope. Do not reframe this as a short-term subset.

## Goal

Implement Gateyes as a gateway, MaaS control plane, Kubernetes CRD platform, and self-hosted inference controller while preserving the gateway hot-path boundary.

## Architecture

The gateway remains the data plane. A new operator/control-plane layer watches CRDs and reconciles them to existing Gateyes Admin API, database, and runtime caches. Standalone, Helm, CRD, and MaaS modes all use the same resource vocabulary.

## Implementation Tasks

### Task 1: Canonical Design and CRD Surface

- Add the multi-deployment design document.
- Add CRDs for `GateyesGateway`, `ModelEndpoint`, `RoutePolicy`, `BudgetPolicy`, `InferenceAutoscalePolicy`, and `InferenceService`.
- Add validation tests that parse the CRD manifests and check core names, versions, scopes, and schema presence.

### Task 2: Shared Platform Resource Types

- Add Go structs under a Kubernetes-free package such as `internal/service/platform`.
- Map CRD fields to existing provider registry, router config, budget policy, and service runtime concepts.
- Add unit tests for conversion to provider and router settings.

### Task 3: Gateway Routing Enhancements

- Add endpoint labels to provider configuration and registry.
- Add request header strategy override with configurable allowlist.
- Add strategies: `least_latency`, `power_of_two`, `least_kv_cache`, `least_gpu_cache`.
- Add score breakdown to route trace.
- Keep prefix/session affinity independent of strategy.

### Task 4: Autoscale Engine

- Add an autoscale evaluator that accepts runtime stats and an `InferenceAutoscalePolicy`.
- Support `observe`, `recommend`, and `enforce` modes at the decision layer.
- Add metrics for desired replicas, observed saturation, and reason.
- Do not mutate Kubernetes workloads until the operator exists.

### Task 5: Operator Skeleton

- Add `cmd/operator` as a separate binary.
- Keep Kubernetes dependencies out of `cmd/gateway`.
- Watch the six CRDs.
- Reconcile CRDs to Gateyes Admin API first; direct DB writes remain optional and behind an interface.
- Update CRD status conditions.

### Task 6: Helm and Deployment Modes

- Wire CRD installation into the Helm chart.
- Add values for `mode: gateway | maas | operator | full`.
- Add optional operator deployment templates.
- Document binary, Docker Compose, Helm, CRD/operator, and full MaaS installation.

### Task 7: MaaS Console and APIs

- Extend model catalog to include CRD/provider origin, health, pricing, capability, and route labels.
- Add tenant-facing usage and budget views.
- Add model/service subscription views.
- Preserve admin RBAC boundaries.

### Task 8: Self-Hosted Inference

- Add `InferenceService` conversion for vLLM/SGLang/KServe/external runtimes.
- Add service discovery and metrics endpoint resolution.
- Add model adapter intent for LoRA-style adapters.
- Feed discovered endpoints into provider registry.

### Task 9: Verification

- Run CRD manifest tests.
- Run focused router/provider tests after routing changes.
- Run Helm template validation where available.
- Run `go test ./...` with repo-local Go caches.

## Acceptance Criteria

- All six CRDs are present and schema-valid.
- Gateway can still build without Kubernetes controller packages in its request path.
- Operator is a separate deployable unit.
- Route policy, budget policy, model endpoint, autoscale policy, and inference service have explicit conversion paths into existing Gateyes runtime concepts.
- Documentation covers every deployment form without treating any requested form as optional future scope.
