# Rollback

## 1. Helm rollback

```bash
helm rollback gateyes <revision> -n gateyes-prod
```

## 2. Database rollback

There is no automatic SQL migration down path today.

Use:

1. application rollback via Helm
2. database restore via backup if schema/data rollback is required

SQLite backup/restore example:

```bash
mkdir -p ./backups
cp ./gateyes.db ./backups/gateyes.db.$(date -u +%Y%m%dT%H%M%SZ).bak
cp ./backups/gateyes.db.<timestamp>.bak ./gateyes.db
```

## 3. Rollback checklist

1. Freeze deployments
2. Roll back Helm release
3. Restore DB if needed
4. Verify `/health` and `/ready`
5. Check request success rate, latency, fallback rate
