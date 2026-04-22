# On-Call Runbook

## 1. Health check fails

1. Check pod/container logs
2. Check DB connectivity
3. Check config mount and secret env presence
4. Verify `/health` and `/ready`

## 2. Error rate spike

1. Check Prometheus alerts
2. Inspect provider-specific request failures
3. Check fallback rate and circuit breaker state
4. Drain/quarantine failing provider if needed
5. Check provider health changes via `/admin/providers` and audit entries via `/admin/audit`

## 3. Budget or quota complaints

1. Check `/admin/usage/*`
2. Check key/project/tenant budget fields
3. Confirm whether rejection is quota or budget

## 4. Slow responses / TTFT spike

1. Check provider latency panels
2. Check upstream provider health
3. Check gateway retry/fallback rate
4. Check DB latency and connection saturation

## 5. Deploy rollback

1. Use Helm rollback
2. Restore DB if schema/data issue exists
3. Re-run smoke checks

## 6. Audit investigation

1. Query `/admin/audit?action=<action>`
2. Filter by `resource_type`, `resource_id`, `actor_user_id`, and time range
3. Cross-check `request_id` with application logs

## 7. Provider health webhook

Provider state changes emit `provider_state_changed` when `alert.providerStateURL` is configured.

Use it to notify SRE channels when providers move between:

1. `healthy`
2. `degraded`
3. `unhealthy`
