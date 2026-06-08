# Upgrade

## 1. Pre-check

1. Review release notes / GitHub release notes
2. Back up database
3. Confirm target image tag exists
4. Confirm Helm values do not contain plaintext secrets

## 2. Kubernetes upgrade

```bash
helm upgrade gateyes ./deploy/helm/gateyes \
  -n gateyes-prod \
  -f ./deploy/helm/gateyes/values.yaml \
  -f ./deploy/helm/gateyes/values-prod.yaml \
  --set image.tag=vX.Y.Z
```

## 3. Verification

1. `kubectl -n gateyes-prod get pods`
2. `kubectl -n gateyes-prod logs deploy/gateyes-gateyes --tail=200`
3. `curl /health`
4. `curl /ready`
5. Check Prometheus/Grafana for error rate and fallback spikes

## 4. Schema strategy

Current migration strategy is forward-only.

That means:

1. Apply backup before upgrade
2. If migration is incompatible, rollback is backup/restore plus app rollback
