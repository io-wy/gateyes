#!/usr/bin/env bash
#
# provision-db.sh — 为 gateyes 在共享 PostgreSQL 上创建/修复 role/database。
# 幂等：可重复执行，不破坏已有数据。
# 用法：bash scripts/provision-db.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="${ENV_FILE:-$REPO_ROOT/.env}"

PG_CONTAINER="${PG_CONTAINER:-postgres}"
PG_SUPERUSER="${PG_SUPERUSER:-postgres}"
PG_SUPERPASS="${PG_SUPERPASS:-postgres}"
PG_HOST="${PG_HOST:-localhost}"
PG_PORT="${PG_PORT:-5432}"

log() { printf '[provision-db] %s\n' "$*"; }
err() { printf '[provision-db] ERROR: %s\n' "$*" >&2; }

# 从 .env 的 GATEYES_DATABASE_DSN 解析 gateyes 角色/密码/库名
if [ ! -f "$ENV_FILE" ]; then
  err "$ENV_FILE not found"
  exit 1
fi
DSN="$(grep "^GATEYES_DATABASE_DSN=" "$ENV_FILE" | head -1 | cut -d= -f2-)"
if [ -z "$DSN" ]; then err "GATEYES_DATABASE_DSN empty"; exit 1; fi

parse_dsn() {
  local key="$1"
  local val
  val="$(echo "$DSN" | grep -oE "$key=[^[:space:]]+" | head -1 | cut -d= -f2-)"
  echo "${val:-}"
}
GATEYES_USER="$(parse_dsn user)"
GATEYES_PASS="$(parse_dsn password)"
GATEYES_DB="$(parse_dsn dbname)"
[ -z "$GATEYES_USER" ] && GATEYES_USER="gateyes"
[ -z "$GATEYES_DB" ]   && GATEYES_DB="gateyes"
if [ -z "$GATEYES_PASS" ]; then err "could not parse password from GATEYES_DATABASE_DSN"; exit 1; fi
log "target role=$GATEYES_USER database=$GATEYES_DB"

# 等待 postgres 就绪
wait_ready() {
  log "waiting for postgres at $PG_HOST:$PG_PORT ..."
  local i
  for i in $(seq 1 60); do
    if docker exec "$PG_CONTAINER" pg_isready -U "$PG_SUPERUSER" -h localhost >/dev/null 2>&1; then
      log "postgres ready"; return 0
    fi
    sleep 1
  done
  err "postgres not ready after 60s"; exit 1
}
wait_ready

# 建 role（幂等）
role_exists() {
  docker exec "$PG_CONTAINER" psql -U "$PG_SUPERUSER" -tAc "SELECT 1 FROM pg_roles WHERE rolname='$GATEYES_USER'" 2>/dev/null | grep -qx 1
}
if role_exists; then
  log "role $GATEYES_USER already exists"
else
  log "creating role $GATEYES_USER"
  docker exec "$PG_CONTAINER" psql -U "$PG_SUPERUSER" -c "CREATE ROLE $GATEYES_USER LOGIN PASSWORD '$GATEYES_PASS';"
fi

# 建 database（幂数）
db_exists() {
  docker exec "$PG_CONTAINER" psql -U "$PG_SUPERUSER" -tAc "SELECT 1 FROM pg_database WHERE datname='$GATEYES_DB'" 2>/dev/null | grep -qx 1
}
if db_exists; then
  log "database $GATEYES_DB already exists"
else
  log "creating database $GATEYES_DB"
  docker exec "$PG_CONTAINER" psql -U "$PG_SUPERUSER" -c "CREATE DATABASE $GATEYES_DB OWNER $GATEYES_USER;"
fi

# PG15+ 关键修复：让 gateyes 拥有 public schema，否则 migrations 报 permission denied
log "fixing public schema ownership for $GATEYES_USER"
docker exec "$PG_CONTAINER" psql -U "$PG_SUPERUSER" -d "$GATEYES_DB" -c \
  "ALTER SCHEMA public OWNER TO $GATEYES_USER; GRANT ALL ON SCHEMA public TO $GATEYES_USER;" >/dev/null

# 自检：以 gateyes 身份建/删临时表
log "self-test: create table as $GATEYES_USER"
docker exec -e "PGPASSWORD=$GATEYES_PASS" "$PG_CONTAINER" psql -U "$GATEYES_USER" -d "$GATEYES_DB" -v ON_ERROR_STOP=1 \
  -c "CREATE TABLE _provision_probe(id int); DROP TABLE _provision_probe; SELECT current_user, current_database(), 'schema writable' AS status;" >/dev/null

log "database provisioning complete: $GATEYES_USER@$GATEYES_DB"
