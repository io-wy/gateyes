# Changelog

## Unreleased

- **BREAKING**: Extract ingress controller, microservice gateway proxy, service discovery (consul/etcd/nacos/k8s), and K8s CRD operator (ProviderReconciler) to archive branch `archive/ingress-and-msgw`. Main branch is now pure AI gateway — no k8s.io/controller-runtime/consul/etcd/nacos dependencies in go.mod, no ingress middleware in request path, provider management via config/DB/Admin API only
- Add CI baseline workflow
- Add release workflow with multi-arch image build, SBOM, signing and provenance
- Add Dockerfile, docker-compose, Helm chart and deployment docs
- Remove committed provider secrets from default config and move examples to env placeholders
