#!/bin/bash
# Gateway API Test Script

GATEWAY_URL="http://localhost:8083"
API_KEY="test-key-001:test-secret"

echo "========================================="
echo "Gateway API Tests"
echo "========================================="

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

# Test function
test_api() {
    local name="$1"
    local cmd="$2"

    echo -n "Testing $name... "
    if eval "$cmd" > /dev/null 2>&1; then
        echo -e "${GREEN}PASS${NC}"
    else
        echo -e "${RED}FAIL${NC}"
    fi
}

# 1. OpenAI Chat Completions - Non-streaming
echo ""
echo "=== OpenAI Chat Completions ==="
curl -s "$GATEWAY_URL/v1/chat/completions" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d '{"model":"MiniMax-M2.5","messages":[{"role":"user","content":"say hi"}],"max_tokens":50}'

# 2. OpenAI Chat Completions - Streaming
echo ""
echo ""
echo "=== OpenAI Streaming ==="
curl -s "$GATEWAY_URL/v1/chat/completions" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d '{"model":"MiniMax-M2.5","messages":[{"role":"user","content":"say hi"}],"max_tokens":50,"stream":true}'

# 3. Anthropic Messages - Non-streaming
echo ""
echo ""
echo "=== Anthropic Messages ==="
curl -s "$GATEWAY_URL/v1/messages" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d '{"model":"MiniMax-M2.5","messages":[{"role":"user","content":"say hi"}],"max_tokens":50}'

# 4. Anthropic Messages - Streaming
echo ""
echo ""
echo "=== Anthropic Streaming ==="
curl -s "$GATEWAY_URL/v1/messages" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d '{"model":"MiniMax-M2.5","messages":[{"role":"user","content":"say hi"}],"max_tokens":50,"stream":true}'

# 5. Tool Calling
echo ""
echo ""
echo "=== Tool Calling ==="
curl -s "$GATEWAY_URL/v1/chat/completions" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d '{
        "model": "MiniMax-M2.5",
        "messages": [{"role": "user", "content": "What is 2+2?"}],
        "tools": [{
            "type": "function",
            "function": {
                "name": "calculator",
                "description": "A calculator",
                "parameters": {
                    "type": "object",
                    "properties": {"expression": {"type": "string"}},
                    "required": ["expression"]
                }
            }
        }]
    }'

# 6. Multi-turn
echo ""
echo ""
echo "=== Multi-turn ==="
curl -s "$GATEWAY_URL/v1/chat/completions" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d '{
        "model": "MiniMax-M2.5",
        "messages": [
            {"role": "user", "content": "My name is Alice"},
            {"role": "assistant", "content": "Hello Alice!"},
            {"role": "user", "content": "What is my name?"}
        ],
        "max_tokens": 50
    }'

echo ""
echo ""
echo "========================================="
echo "Tests Complete"
echo "========================================="
