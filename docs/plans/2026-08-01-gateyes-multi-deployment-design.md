# Gateyes Multi-Deployment Design

> Status: approved for implementation by the user on 2026-08-01.

## Goal

Gateyes should become a full LLM gateway and MaaS control plane with several deployment forms:

- Standalone gateway for binary, VM, Docker, and Docker Compose deployments.
- Helm gateway for production Kubernetes deployments.
- Kubernetes CRD/operator mode for declarative platform operation.
- MaaS platform mode for tenant-facing model access, quotas, usage, and service publishing.
- Self-hosted inference control for vLLM, SGLang, KServe, and external OpenAI-compatible runtimes.

This is one product surface, not separate short-term and long-term tracks. Every mode uses the same resource model and the same gateway data plane.

## Non-Negotiable Boundary

The gateway request path stays pure AI gateway code. It must not import Kubernetes controller libraries, query the Kubernetes API per request, or turn into a general ingress controller again.

Kubernetes integration is implemented as an optional control-plane layer:

```text
K8s CRDs -> gateyes-operator -> Gateyes Admin API / database / config cache
                                      |
                                      v
Client -> Gateyes Gateway data plane -> providers / model endpoints
```

This keeps the existing gateway deployable outside Kubernetes while still allowing GitOps and platform automation inside Kubernetes.

## Product Modes

| Mode | Primary user | Delivery | Capabilities |
| --- | --- | --- | --- |
| Standalone Gateway | App teams and small private deployments | binary, Docker, Docker Compose | Provider routing, auth, budget, cache, plugin, audit, batch inference |
| Helm Gateway | Kubernetes platform teams | Helm chart | Production gateway, admin console, migrations, metrics, alerts, HPA |
| CRD/Operator Mode | Kubernetes platform teams | CRDs plus optional operator | Declarative gateway, provider, route, quota, autoscale, inference service state |
| MaaS Platform | Internal AI platform owners | Gateway plus admin console | Tenants, projects, model catalog, virtual keys, usage, chargeback, service publishing |
| Self-Hosted Inference | GPU platform owners | CRD or Helm-managed runtimes | vLLM/SGLang/KServe endpoint discovery, runtime metrics, autoscaling, model adapters |

## Resource Model

The CRDs are the canonical platform resources. Standalone and MaaS modes map the same concepts to YAML config, database rows, and Admin API payloads.

| CRD | Gateway mapping | Purpose |
| --- | --- | --- |
| `GateyesGateway` | server/admin/dependencies config | Deploy and configure gateway data plane and admin plane |
| `ModelEndpoint` | provider registry | Declare cloud API, vLLM, SGLang, KServe, or external model endpoint |
| `RoutePolicy` | router rule engine and strategy config | Select endpoints by model, tenant, labels, headers, cost, latency, or runtime state |
| `BudgetPolicy` | tenant/project/key/service budget config | Enforce RPM, TPM, QPS, token budget, and money budget |
| `InferenceAutoscalePolicy` | runtime stats plus scaler decisions | Observe, recommend, or enforce scaling from LLM-specific signals |
| `InferenceService` | local runtime provider plus deployment intent | Deploy or describe self-hosted vLLM/SGLang/KServe services |

## Control Plane Responsibilities

The control plane owns:

- CRUD and validation of resource intent.
- Sync from CRD objects to Gateyes provider, route, quota, and service runtime state.
- K8s service/pod discovery for `ModelEndpoint.serviceRef`.
- Runtime metric scraping and autoscale decisions.
- Status conditions for GitOps users.
- Drift detection between CRDs and gateway runtime state.

The data plane owns:

- OpenAI/Anthropic-compatible request handling.
- Hot path auth, quota, budget, routing, cache, plugin execution, audit, and response persistence.
- Local cached provider and route state.
- Fail-open behavior when optional runtime signals are stale.

## Routing Requirements

Gateyes routing must grow from provider ordering to policy-driven inference routing:

- Header-level strategy override, with admin-controlled allowlists.
- Endpoint labels for region, accelerator, runtime, cost tier, tenant scope, and model family.
- Strategy support for `least_latency`, `power_of_two`, `least_kv_cache`, and `least_gpu_cache`.
- Score explanation in route trace so users can see why an endpoint won.
- Prefix/session affinity remains separate from strategy.
- Runtime metrics remain cached outside the request path.

## Autoscaling Requirements

Autoscaling is a first-class feature, not only documentation. It has three execution modes:

- `observe`: collect metrics and publish status only.
- `recommend`: calculate desired replicas but do not mutate workloads.
- `enforce`: mutate a target deployment, stateful set, or inference service.

Autoscale inputs include queue depth, active requests, TTFT, p95 latency, error rate, TPM, RPM, GPU utilization, GPU KV cache usage, CPU KV cache usage, and cache hit rate.

## MaaS Requirements

MaaS mode is not generic MLOps. It must focus on model consumption:

- Model catalog, capabilities, pricing, and health.
- Tenant, project, API key, virtual key, and service ownership.
- Quota, budget, chargeback, and usage exports.
- Admin and tenant-facing playground.
- Service catalog with prompt/service versioning, publish, rollback, and subscription.
- Audit logs and route traces.

Training jobs, datasets, experiment tracking, notebooks, feature stores, and model artifact registry are out of scope unless they directly serve self-hosted inference deployment.

## Implementation Invariants

- `cmd/gateway` remains Kubernetes-free.
- CRDs live under the Helm chart so Kubernetes users can install them declaratively.
- The operator, when added, lives in a separate command/module boundary.
- All CRD objects must have status conditions.
- All runtime status that can affect routing must be cached before the request path.
- Standalone config, Admin API, and CRD objects must converge on the same provider and policy vocabulary.
