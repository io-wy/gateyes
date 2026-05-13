#!/bin/bash
# Gateyes 流量生成器 —— 产生真实多样的 metrics 数据

GATEWAY="http://localhost:8028"
DEMO_KEY="demo-key-001:demo-secret-001"
ADMIN_KEY="admin-key-001:admin-secret-001"

call_chat() {
  local model=$1
  local content=${2:-"hello"}
  curl -s -X POST \
    -H "Authorization: Bearer $DEMO_KEY" \
    -H "Content-Type: application/json" \
    "$GATEWAY/v1/chat/completions" \
    -d "{\"model\":\"$model\",\"messages\":[{\"role\":\"user\",\"content\":\"$content\"}],\"max_tokens\":10}" \
    > /dev/null 2>&1
}

call_anthropic() {
  curl -s -X POST \
    -H "Authorization: Bearer $DEMO_KEY" \
    -H "Content-Type: application/json" \
    -H "anthropic-version: 2023-06-01" \
    "$GATEWAY/v1/messages" \
    -d '{"model":"glm-5.1","messages":[{"role":"user","content":"hi"}],"max_tokens":10}' \
    > /dev/null 2>&1
}

call_models() {
  curl -s -H "Authorization: Bearer $DEMO_KEY" "$GATEWAY/v1/models" > /dev/null 2>&1
}

call_admin() {
  curl -s -u "$ADMIN_KEY" "$GATEWAY/admin/providers" > /dev/null 2>&1
  curl -s -u "$ADMIN_KEY" "$GATEWAY/admin/dashboard" > /dev/null 2>&1
}

call_bad_auth() {
  curl -s -X POST \
    -H "Authorization: Bearer bad-key:bad-secret" \
    -H "Content-Type: application/json" \
    "$GATEWAY/v1/chat/completions" \
    -d '{"model":"LongCat-Flash-Chat","messages":[{"role":"user","content":"x"}]}' \
    > /dev/null 2>&1
}

call_rate_limit() {
  for i in $(seq 1 30); do
    curl -s -X POST \
      -H "Authorization: Bearer $DEMO_KEY" \
      -H "Content-Type: application/json" \
      "$GATEWAY/v1/chat/completions" \
      -d '{"model":"LongCat-Flash-Chat","messages":[{"role":"user","content":"r"}],"max_tokens":1}' \
      > /dev/null 2>&1 &
  done
  wait
}

echo "Starting traffic generator..."
while true; do
  call_chat "LongCat-Flash-Chat" "hello world"
  sleep 0.5

  call_chat "LongCat-Flash-Chat" "what is kubernetes"
  sleep 0.5

  call_anthropic
  sleep 0.5

  call_models
  sleep 0.5

  call_admin
  sleep 0.5

  call_bad_auth
  sleep 0.5

  call_rate_limit
  sleep 2

done
