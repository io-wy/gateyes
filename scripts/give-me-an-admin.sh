#!/usr/bin/env bash
#
# give-me-an-admin.sh — 一键把 gateyes 从零冷启动到「可开始测试」。
# 全自动、幂等、可重入：确保依赖 -> provision DB -> 起网关 -> 验证 admin -> 打印凭证。
# 用法：bash scripts/give-me-an-admin.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="${ENV_FILE:-$REPO_ROOT/.env}"
CONFIG="${CONFIG:-configs/config.yaml}"
CONFIG_PATH="$REPO_ROOT/$CONFIG"
LOG="${GATEYES_LOG:-/tmp/gateyes.log}"

PG_CONTAINER="${PG_CONTAINER:-postgres}"
PG_IMAGE="${PG_IMAGE:-postgres:16-alpine}"
PG_VOLUME="${PG_VOLUME:-pgdata}"
REDIS_CONTAINER="${REDIS_CONTAINER:-gateyes-redis}"
REDIS_IMAGE="${REDIS_IMAGE:-redis:7-alpine}"

log() { printf '[give-me-an-admin] %s\n' "$*"; }
ok()  { printf '[give-me-an-admin] OK: %s\n' "$*"; }
warn(){ printf '[give-me-an-admin] WARN: %s\n' "$*"; }
err() { printf '[give-me-an-admin] ERROR: %s\n' "$*" >&2; }

# 1. 校验 .env（不自动覆盖）
if [ ! -f "$ENV_FILE" ]; then
  err "$ENV_FILE 不存在。请先: cp .env.example .env 并按需填写"
  exit 1
fi

# 2. 从 config / .env 读关键值
PORT="$(grep -E '^[[:space:]]*listenAddr:' "$CONFIG_PATH" | head -1 | grep -oE '[0-9]+' | head -1)"
: "${PORT:=8028}"
GATEYES_URL="http://localhost:$PORT"
ADMIN_KEY="$(grep -E '^[[:space:]]*bootstrapKey:' "$CONFIG_PATH" | head -1 | awk '{print $2}')"
: "${ADMIN_KEY:=admin-key-001}"
env_val() { grep "^$1=" "$ENV_FILE" | head -1 | cut -d= -f2-; }
ADMIN_SECRET="$(env_val GATEYES_ADMIN_BOOTSTRAP_SECRET)"
DEMO_SECRET="$(env_val GATEYES_DEMO_SECRET)"
if [ -z "$ADMIN_SECRET" ]; then err "GATEYES_ADMIN_BOOTSTRAP_SECRET 未在 .env 设置"; exit 1; fi

# 3. 确保依赖容器
ensure_container() { # $1=name ; 其余=缺失时的创建命令
  local name="$1"; shift
  if docker ps --format '{{.Names}}' | grep -qx "$name"; then
    log "容器 $name 已在运行"
  elif docker ps -a --format '{{.Names}}' | grep -qx "$name"; then
    log "启动已存在容器 $name"; docker start "$name" >/dev/null
  else
    log "创建容器 $name"; "$@" >/dev/null
  fi
}
ensure_container "$PG_CONTAINER" docker run -d --name "$PG_CONTAINER" \
  -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=postgres \
  -p 5432:5432 -v "$PG_VOLUME":/var/lib/postgresql/data "$PG_IMAGE"
ensure_container "$REDIS_CONTAINER" docker run -d --name "$REDIS_CONTAINER" \
  -p 6379:6379 "$REDIS_IMAGE"

# 4. provision DB
log "provisioning 数据库..."
bash "$SCRIPT_DIR/provision-db.sh"

# 5. 清理残留 gateway（lsof 精确杀）
log "清理 :$PORT / :6060 残留进程..."
for p in $(lsof -nP -iTCP:"$PORT" -iTCP:6060 -sTCP:LISTEN 2>/dev/null | grep -v COMMAND | awk '{print $2}' | sort -u); do
  kill -9 "$p" 2>/dev/null && log "  killed $p" || true
done
sleep 1

# 6. 后台启动 gateway
log "启动 gateway (config=$CONFIG)，日志 -> $LOG"
: > "$LOG"
( cd "$REPO_ROOT" && nohup go run ./cmd/gateway -config "$CONFIG" >"$LOG" 2>&1 & echo $! >/tmp/gateyes.pid )
disown 2>/dev/null || true

# 7. 轮询 /ready
log "等待 gateway 就绪 ($GATEYES_URL/ready)..."
up=0
for _ in $(seq 1 90); do
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$GATEYES_URL/ready" 2>/dev/null || true)"
  if [ "$code" = "200" ] || [ "$code" = "204" ]; then up=1; break; fi
  if grep -qiE 'permission denied for schema|failed to (migrate|seed|ensure)|FATAL' "$LOG" 2>/dev/null; then
    err "gateway 启动失败，日志关键行："
    grep -iE 'permission denied|failed to|FATAL|error' "$LOG" | tail -8
    exit 1
  fi
  sleep 1
done
if [ "$up" -ne 1 ]; then
  err "gateway 90s 内未就绪，日志尾部："; tail -15 "$LOG"; exit 1
fi
ok "gateway 已就绪"

# 8. 验证 admin
admin_code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 \
  "$GATEYES_URL/admin/v1/keys" -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET" 2>/dev/null || true)"
if [ "$admin_code" = "200" ]; then
  ok "admin 鉴权通过 (GET /admin/v1/keys -> 200)"
else
  warn "admin 鉴权返回 $admin_code（预期 200）。检查 GATEYES_ADMIN_BOOTSTRAP_SECRET 是否与库一致。"
fi

# 9. 交付信息
PID="$(cat /tmp/gateyes.pid 2>/dev/null || echo '?')"
cat <<INFO

========================================================================
 Gateyes 冷启动完成，可以开始测试了
========================================================================
 URL          : $GATEYES_URL
 网关 PID     : $PID   (停止: kill \$(cat /tmp/gateyes.pid))
 日志         : $LOG

 管理员 (admin, bootstrap 短路):
   Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET
 业务测试 key (demo, 已 seed 入库):
   Authorization: Bearer demo-key-001:${DEMO_SECRET:-<见 .env GATEYES_DEMO_SECRET>}

 试一下：
   curl -s $GATEYES_URL/admin/v1/keys \\
     -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET" | jq .

   curl -s -X POST $GATEYES_URL/v1/chat/completions \\
     -H "Authorization: Bearer demo-key-001:${DEMO_SECRET:-<secret>}" \\
     -H 'Content-Type: application/json' \\
     -d '{"model":"gpt-5.4","messages":[{"role":"user","content":"ping"}]}'
========================================================================
INFO
