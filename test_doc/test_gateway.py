#!/usr/bin/env python3
"""
Gateway API Test Script
"""
import sys
import io

# Fix Windows console encoding
if sys.platform == 'win32':
    sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8', errors='replace')
    sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding='utf-8', errors='replace')

import argparse
import json
import urllib.request
import urllib.error


def make_request(url, headers, data):
    """Make HTTP request and return response."""
    req = urllib.request.Request(
        url,
        data=json.dumps(data).encode('utf-8'),
        headers=headers,
        method='POST'
    )
    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        print(f"Error: {e.code} - {e.read().decode('utf-8')}")
        return None


def test_openai_chat(args):
    """Test OpenAI Chat Completions API."""
    print("\n=== Test: OpenAI Chat Completions (Non-streaming) ===")
    url = f"{args.url}/v1/chat/completions"
    headers = {
        "Authorization": f"Bearer {args.api_key}",
        "Content-Type": "application/json"
    }
    data = {
        "model": "MiniMax-M2.5",
        "messages": [{"role": "user", "content": "say hi"}],
        "max_tokens": 50
    }

    result = make_request(url, headers, data)
    if result and "choices" in result:
        content = result["choices"][0]["message"]["content"]
        print(f"Response: {content}")
        return True
    return False


def test_openai_streaming(args):
    """Test OpenAI Chat Completions Streaming."""
    print("\n=== Test: OpenAI Chat Completions (Streaming) ===")
    print("(Streaming response, partial output shown)")

    url = f"{args.url}/v1/chat/completions"
    headers = {
        "Authorization": f"Bearer {args.api_key}",
        "Content-Type": "application/json"
    }
    data = {
        "model": "MiniMax-M2.5",
        "messages": [{"role": "user", "content": "say hi"}],
        "max_tokens": 50,
        "stream": True
    }

    req = urllib.request.Request(
        url,
        data=json.dumps(data).encode('utf-8'),
        headers=headers,
        method='POST'
    )

    try:
        with urllib.request.urlopen(req) as resp:
            # Read partial response
            import select
            import sys
            while True:
                line = resp.readline()
                if not line:
                    break
                line = line.decode('utf-8')
                if line.strip():
                    print(line.strip()[:100])
                    if "[DONE]" in line:
                        break
        return True
    except Exception as e:
        print(f"Error: {e}")
        return False


def test_anthropic_messages(args):
    """Test Anthropic Messages API."""
    print("\n=== Test: Anthropic Messages (Non-streaming) ===")
    url = f"{args.url}/v1/messages"
    headers = {
        "Authorization": f"Bearer {args.api_key}",
        "Content-Type": "application/json"
    }
    data = {
        "model": "MiniMax-M2.5",
        "messages": [{"role": "user", "content": "say hi"}],
        "max_tokens": 50
    }

    result = make_request(url, headers, data)
    if result and "content" in result:
        # Get text content (skip thinking block)
        for block in result["content"]:
            if block.get("type") == "text":
                print(f"Response: {block['text']}")
                return True
    return False


def test_anthropic_streaming(args):
    """Test Anthropic Messages Streaming."""
    print("\n=== Test: Anthropic Messages (Streaming) ===")
    print("(Streaming response, partial output shown)")

    url = f"{args.url}/v1/messages"
    headers = {
        "Authorization": f"Bearer {args.api_key}",
        "Content-Type": "application/json"
    }
    data = {
        "model": "MiniMax-M2.5",
        "messages": [{"role": "user", "content": "say hi"}],
        "max_tokens": 50,
        "stream": True
    }

    req = urllib.request.Request(
        url,
        data=json.dumps(data).encode('utf-8'),
        headers=headers,
        method='POST'
    )

    try:
        with urllib.request.urlopen(req) as resp:
            while True:
                line = resp.readline()
                if not line:
                    break
                line = line.decode('utf-8')
                if line.strip():
                    print(line.strip()[:100])
                    if "[DONE]" in line:
                        break
        return True
    except Exception as e:
        print(f"Error: {e}")
        return False


def test_tool_calling(args):
    """Test Tool Calling."""
    print("\n=== Test: Tool Calling ===")
    url = f"{args.url}/v1/chat/completions"
    headers = {
        "Authorization": f"Bearer {args.api_key}",
        "Content-Type": "application/json"
    }
    data = {
        "model": "MiniMax-M2.5",
        "messages": [{"role": "user", "content": "What is 2+2?"}],
        "tools": [{
            "type": "function",
            "function": {
                "name": "calculator",
                "description": "A calculator",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "expression": {"type": "string"}
                    },
                    "required": ["expression"]
                }
            }
        }]
    }

    result = make_request(url, headers, data)
    if result and "choices" in result:
        msg = result["choices"][0]["message"]
        if "tool_calls" in msg:
            tool = msg["tool_calls"][0]["function"]
            print(f"Tool call: {tool['name']}({tool['arguments']})")
            return True
        else:
            print(f"Response: {msg.get('content', '')}")
            return True
    return False


def test_multiturn(args):
    """Test Multi-turn conversation."""
    print("\n=== Test: Multi-turn Conversation ===")
    url = f"{args.url}/v1/chat/completions"
    headers = {
        "Authorization": f"Bearer {args.api_key}",
        "Content-Type": "application/json"
    }
    data = {
        "model": "MiniMax-M2.5",
        "messages": [
            {"role": "user", "content": "My name is Alice"},
            {"role": "assistant", "content": "Hello Alice! Nice to meet you."},
            {"role": "user", "content": "What is my name?"}
        ],
        "max_tokens": 50
    }

    result = make_request(url, headers, data)
    if result and "choices" in result:
        content = result["choices"][0]["message"]["content"]
        print(f"Response: {content}")
        return True
    return False


def main():
    parser = argparse.ArgumentParser(description="Gateway API Test Script")
    parser.add_argument("--url", default="http://localhost:8083", help="Gateway URL")
    parser.add_argument("--api-key", default="test-key-001:test-secret", help="API Key")

    args = parser.parse_args()

    print("=" * 50)
    print("Gateway API Tests")
    print("=" * 50)
    print(f"URL: {args.url}")
    print(f"API Key: {args.api_key}")

    tests = [
        ("OpenAI Chat", lambda: test_openai_chat(args)),
        ("OpenAI Streaming", lambda: test_openai_streaming(args)),
        ("Anthropic Messages", lambda: test_anthropic_messages(args)),
        ("Anthropic Streaming", lambda: test_anthropic_streaming(args)),
        ("Tool Calling", lambda: test_tool_calling(args)),
        ("Multi-turn", lambda: test_multiturn(args)),
    ]

    results = []
    for name, test_fn in tests:
        try:
            results.append((name, test_fn()))
        except Exception as e:
            print(f"Error in {name}: {e}")
            results.append((name, False))

    print("\n" + "=" * 50)
    print("Results Summary")
    print("=" * 50)
    passed = sum(1 for _, r in results if r)
    total = len(results)
    for name, result in results:
        status = "PASS" if result else "FAIL"
        print(f"{name}: {status}")
    print(f"\nTotal: {passed}/{total} passed")


if __name__ == "__main__":
    main()
