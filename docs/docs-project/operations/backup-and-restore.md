# Backup And Restore

## 1. SQLite

Use SQLite only for local/dev or very small single-instance deployments.

### Backup

```bash
mkdir -p ./backups && cp ./gateyes.db ./backups/gateyes.db.$(date -u +%Y%m%dT%H%M%SZ).bak
```

### Restore

```bash
cp ./backups/gateyes.db.<timestamp>.bak ./gateyes.db
```

### Notes

1. Put the database file on a persistent volume
2. Stop writes before file copy if you need strict consistency

## 2. Postgres / MySQL

Recommended for staging/prod.

Minimum expectations:

1. scheduled backups
2. restore drill in staging
3. retention policy
4. encryption at rest

## 3. Migration rollback strategy

Current strategy is:

1. backup first
2. migrate forward
3. rollback via application image + database restore

There is no general `down` migration automation in the repository today.
