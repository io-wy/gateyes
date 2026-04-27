#!/usr/bin/env bash
#
# Gateyes 完整功能体验脚本
# 零 API 成本，基于 mock upstream
#
# 前置要求：
#   1. Docker + Docker Compose 已安装
#   2. Go 1.23+ 已安装
#   3. 在 Git Bash / WSL / Linux 环境中运行
#
# 使用方法：
#   ./scripts/demo-walkthrough.sh

set -euo pipefail

GATEWAY_URL="http://127.0.0.1:8083"
ADMIN_KEY="admin-key-001"
ADMIN_SECRET="admin-secret-001"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

section() {
    echo ""
    echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
}

step() {
    echo ""
    echo -e "${YELLOW}▶ $1${NC}"
}

success() {
    echo -e "${GREEN}✓ $1${NC}"
}

warn() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

wait_for() {
    local url=$1
    local max=${2:-30}
    echo -n "Waiting for $url ..."
    for i in $(seq 1 $max); do
        if curl -sf "$url" >/dev/null 2>&1; then
            echo " OK"
            return 0
        fi
        sleep 1
        echo -n "."
    done
    echo " TIMEOUT"
    return 1
}

curl_json() {
    curl -s "$@" | python3 -m json.tool 2>/dev/null || curl -s "$@"
}

# ════════════════════════════════════════════════════════════
# 阶段 0：前置检查
# ════════════════════════════════════════════════════════════
section "阶段 0：前置检查"

step "检查 Docker"
if ! docker compose version >/dev/null 2>&1; then
    echo "Docker Compose 未安装，请先安装"
    exit 1
fi
success "Docker Compose 可用"

step "检查 Go"
if ! go version >/dev/null 2>&1; then
    echo "Go 未安装，请先安装 Go 1.23+"
    exit 1
fi
success "Go 可用"

step "检查项目目录"
if [[ ! -f "configs/demo-mock.yaml" ]]; then
    echo "请在项目根目录运行此脚本"
    exit 1
fi
success "项目目录正确"

# ════════════════════════════════════════════════════════════
# 阶段 1：启动基础设施
# ════════════════════════════════════════════════════════════
section "阶段 1：启动基础设施 (PostgreSQL + Redis + Prometheus + Grafana)"

step "停止旧服务"
docker compose down --volumes --remove-orphans 2>/dev/null || true

step "启动基础设施（不含 gateway）"
docker compose up postgres redis prometheus grafana -d --build

step "等待服务就绪"
wait_for "http://127.0.0.1:8083/health" 5 || true  # gateway 还没启动
wait_for "http://127.0.0.1:9090/-/healthy" 30
success "Prometheus 就绪"

# ════════════════════════════════════════════════════════════
# 阶段 2：启动 Mock Upstream
# ════════════════════════════════════════════════════════════
section "阶段 2：启动 Mock Upstream"

step "编译并启动 mock upstream（后台运行）"
if pgrep -f "mockupstream" >/dev/null 2>&1; then
    pkill -f "mockupstream" || true
    sleep 1
fi
go run ./benchmark/mockupstream/main.go -port 19999 &
MOCK_PID=$!
sleep 2

if ! curl -sf "http://127.0.0.1:19999/health" >/dev/null 2>&1; then
    warn "Mock upstream 未响应，请检查端口 19999 是否被占用"
    kill $MOCK_PID 2>/dev/null || true
    exit 1
fi
success "Mock upstream 运行在 :19999 (PID: $MOCK_PID)"

# ════════════════════════════════════════════════════════════
# 阶段 3：启动 Gateway
# ════════════════════════════════════════════════════════════
section "阶段 3：启动 Gateway"

step "编译并启动 gateway（后台运行）"
if pgrep -f "gateway -config configs/demo-mock.yaml" >/dev/null 2>&1; then
    pkill -f "gateway -config configs/demo-mock.yaml" || true
    sleep 1
fi
go run ./cmd/gateway/main.go -config configs/demo-mock.yaml &
GATEWAY_PID=$!
sleep 3

wait_for "http://127.0.0.1:8083/health" 15
success "Gateway 就绪 (PID: $GATEWAY_PID)"

step "检查 Prometheus 指标出口"
curl -sf "http://127.0.0.1:8083/metrics" >/dev/null 2>&1 && success "Prometheus /metrics 可用" || warn "/metrics 暂不可用"

# ════════════════════════════════════════════════════════════
# 阶段 4：管理面初始化
# ════════════════════════════════════════════════════════════
section "阶段 4：管理面初始化"

step "4.1 查看默认租户"
curl_json "$GATEWAY_URL/admin/tenants" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET"

step "4.2 创建新租户"
TENANT_RESP=$(curl -s "$GATEWAY_URL/admin/tenants" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"slug":"demo-team","name":"Demo Team"}')
echo "$TENANT_RESP" | python3 -m json.tool 2>/dev/null || echo "$TENANT_RESP"
TENANT_ID=$(echo "$TENANT_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")

step "4.3 创建用户（自动生成 api_key + api_secret）"
USER_RESP=$(curl -s "$GATEWAY_URL/admin/users" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"name":"alice","role":"tenant_user","tenant_id":"'$TENANT_ID'"}')
echo "$USER_RESP" | python3 -m json.tool 2>/dev/null || echo "$USER_RESP"
USER_KEY=$(echo "$USER_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('api_key',''))" 2>/dev/null || echo "")
USER_SECRET=$(echo "$USER_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('api_secret',''))" 2>/dev/null || echo "")

if [[ -z "$USER_KEY" || -z "$USER_SECRET" ]]; then
    warn "未能提取用户凭证，使用 bootstrap 凭证继续"
    USER_KEY="demo-key-001"
    USER_SECRET="demo-secret-001"
fi
echo "  User API Key:    $USER_KEY"
echo "  User API Secret: $USER_SECRET"

step "4.4 创建项目"
PROJECT_RESP=$(curl -s "$GATEWAY_URL/admin/projects" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"name":"Demo Project","tenant_id":"'$TENANT_ID'","budget_usd":10.0,"budget_policy":"hard_reject"}')
echo "$PROJECT_RESP" | python3 -m json.tool 2>/dev/null || echo "$PROJECT_RESP"
PROJECT_ID=$(echo "$PROJECT_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")

step "4.5 查看 Provider 列表"
curl_json "$GATEWAY_URL/admin/providers" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET"

step "4.6 手动触发 Provider 健康检查"
curl_json -X POST "$GATEWAY_URL/admin/providers/check" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET" \
    -H "Content-Type: application/json" \
    -d '{}'

# ════════════════════════════════════════════════════════════
# 阶段 5：请求面体验 —— 四大 API 入口
# ════════════════════════════════════════════════════════════
section "阶段 5：请求面体验 —— 四大 API 入口"

step "5.1 Responses API（主入口，非流式）"
curl_json -X POST "$GATEWAY_URL/v1/responses" \
    -H "Authorization: Bearer $USER_KEY:$USER_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"model":"mock-model","input":"What is AI?","max_output_tokens":50}'

step "5.2 Responses API（流式 SSE）"
echo "  （以下输出为 SSE 原始事件）"
curl -s -N -X POST "$GATEWAY_URL/v1/responses" \
    -H "Authorization: Bearer $USER_KEY:$USER_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"model":"mock-model","input":"Hello","max_output_tokens":50,"stream":true}' | head -20
echo ""

step "5.3 Chat Completions（兼容层，非流式）"
curl_json -X POST "$GATEWAY_URL/v1/chat/completions" \
    -H "Authorization: Bearer $USER_KEY:$USER_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"model":"mock-model","messages":[{"role":"user","content":"Hello"}],"max_tokens":50}'

step "5.4 Chat Completions（流式 SSE）"
echo "  （SSE 原始事件）"
curl -s -N -X POST "$GATEWAY_URL/v1/chat/completions" \
    -H "Authorization: Bearer $USER_KEY:$USER_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"model":"mock-model","messages":[{"role":"user","content":"Hello"}],"max_tokens":50,"stream":true}' | head -20
echo ""

step "5.5 Messages（Anthropic 兼容，非流式）"
curl_json -X POST "$GATEWAY_URL/v1/messages" \
    -H "Authorization: Bearer $USER_KEY:$USER_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"model":"mock-model","messages":[{"role":"user","content":"Hello"}],"max_tokens":50}'

step "5.6 Messages（流式 SSE）"
echo "  （SSE 原始事件）"
curl -s -N -X POST "$GATEWAY_URL/v1/messages" \
    -H "Authorization: Bearer $USER_KEY:$USER_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"model":"mock-model","messages":[{"role":"user","content":"Hello"}],"max_tokens":50,"stream":true}' | head -20
echo ""

step "5.7 Embeddings"
curl_json -X POST "$GATEWAY_URL/v1/embeddings" \
    -H "Authorization: Bearer $USER_KEY:$USER_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"model":"mock-model","input":["hello","world"]}'

step "5.8 Models 列表"
curl_json "$GATEWAY_URL/v1/models" \
    -H "Authorization: Bearer $USER_KEY:$USER_SECRET"

# ════════════════════════════════════════════════════════════
# 阶段 6：治理体验 —— 限流 / 预算 / Virtual Key
# ════════════════════════════════════════════════════════════
section "阶段 6：治理体验"

step "6.1 查看预算状态"
curl_json "$GATEWAY_URL/admin/budgets" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET"

step "6.2 创建 Virtual Key（受限子凭证）"
VK_RESP=$(curl -s "$GATEWAY_URL/admin/virtual-keys" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"api_key_id":"'$USER_KEY'","budget_usd":0.01,"budget_policy":"hard_reject","rate_limit_qps":5,"allowed_models":["mock-model"]}')
echo "$VK_RESP" | python3 -m json.tool 2>/dev/null || echo "$VK_RESP"
VK_ID=$(echo "$VK_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")

step "6.3 用 Virtual Key 请求（应成功）"
if [[ -n "$VK_ID" ]]; then
    VK_DETAIL=$(curl -s "$GATEWAY_URL/admin/virtual-keys/$VK_ID" \
        -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET")
    VK_KEY=$(echo "$VK_DETAIL" | python3 -c "import sys,json; print(json.load(sys.stdin).get('virtual_key',''))" 2>/dev/null || echo "")
    VK_SECRET=$(echo "$VK_DETAIL" | python3 -c "import sys,json; print(json.load(sys.stdin).get('secret',''))" 2>/dev/null || echo "")
    if [[ -n "$VK_KEY" && -n "$VK_SECRET" ]]; then
        curl_json -X POST "$GATEWAY_URL/v1/responses" \
            -H "Authorization: Bearer $VK_KEY:$VK_SECRET" \
            -H "Content-Type: application/json" \
            -d '{"model":"mock-model","input":"Test VK","max_output_tokens":50}'
    fi
fi

step "6.4 触发限流（快速发送 20 个请求）"
echo "  发送 20 个并发请求..."
for i in $(seq 1 20); do
    curl -s -o /dev/null -w "%{http_code} " -X POST "$GATEWAY_URL/v1/responses" \
        -H "Authorization: Bearer $USER_KEY:$USER_SECRET" \
        -H "Content-Type: application/json" \
        -d '{"model":"mock-model","input":"x","max_output_tokens":10}' &
done
wait
echo ""
success "并发请求完成（如果有 429 说明限流生效）"

# ════════════════════════════════════════════════════════════
# 阶段 7：路由体验 —— 多 Provider / 健康检查 / 动态管理
# ════════════════════════════════════════════════════════════
section "阶段 7：路由与 Provider 管理"

step "7.1 查看 Provider 统计"
curl_json "$GATEWAY_URL/admin/providers/mock-primary/stats" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET"

step "7.2 动态添加一个新 Provider"
curl_json -X POST "$GATEWAY_URL/admin/providers" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET" \
    -H "Content-Type: application/json" \
    -d '{
        "name":"mock-dynamic",
        "type":"openai",
        "vendor":"openai",
        "base_url":"http://localhost:19999/v1",
        "api_key":"mock-key",
        "model":"mock-model",
        "weight":5,
        "price_input":0.000001,
        "price_output":0.000005,
        "max_tokens":8192,
        "timeout":60,
        "enabled":true,
        "capabilities":{"chat":true,"responses":true,"stream":true}
    }'

step "7.3 查看更新后的 Provider 列表"
curl_json "$GATEWAY_URL/admin/providers" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET"

step "7.4 将 mock-failing 标记为 drain（优雅摘除）"
curl_json -X PUT "$GATEWAY_URL/admin/providers/mock-failing" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"enabled":false}'

step "7.5 配置热重载"
curl_json -X POST "$GATEWAY_URL/admin/reload" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET"

# ════════════════════════════════════════════════════════════
# 阶段 8：监控与审计
# ════════════════════════════════════════════════════════════
section "阶段 8：监控与审计"

step "8.1 拉取 Prometheus 指标（关键指标）"
curl -s "$GATEWAY_URL/metrics" | grep -E "gateway_llm_requests_total|gateway_llm_request_duration_seconds|gateway_llm_tokens_total" | head -20 || true

step "8.2 查看审计日志"
curl_json "$GATEWAY_URL/admin/audit" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET"

step "8.3 查看用量汇总"
curl_json "$GATEWAY_URL/admin/usage/summary" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET"

step "8.4 查看用量趋势"
curl_json "$GATEWAY_URL/admin/usage/trend" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET"

step "8.5 查看 Dashboard"
curl_json "$GATEWAY_URL/admin/dashboard" \
    -H "Authorization: Bearer $ADMIN_KEY:$ADMIN_SECRET"

step "8.6 Grafana（浏览器打开）"
echo "  Grafana URL: http://127.0.0.1:3000 (admin/admin)"
echo "  Prometheus:  http://127.0.0.1:9090"

# ════════════════════════════════════════════════════════════
# 阶段 9：清理
# ════════════════════════════════════════════════════════════
section "阶段 9：清理"

step "停止 Gateway"
kill $GATEWAY_PID 2>/dev/null || true
success "Gateway 已停止"

step "停止 Mock Upstream"
kill $MOCK_PID 2>/dev/null || true
success "Mock upstream 已停止"

step "停止基础设施"
docker compose down --volumes --remove-orphans
success "基础设施已清理"

echo ""
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}  体验完成！${NC}"
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
