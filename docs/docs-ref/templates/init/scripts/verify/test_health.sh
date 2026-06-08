#!/bin/bash
set -e

echo "=== Verify: Health Endpoints ==="

# Build the binary first
make build >/dev/null 2>&1

# Start server in background (if config exists)
if [ -f configs/config.yaml ]; then
  ./bin/go-backend-starter -config configs/config.yaml &
  PID=$!
  sleep 2

  # Test health endpoint
  curl -sf http://localhost:8080/health >/dev/null && echo "✓ /health OK" || echo "✗ /health FAILED"

  # Test ready endpoint
  curl -sf http://localhost:8080/ready >/dev/null && echo "✓ /ready OK" || echo "✗ /ready FAILED"

  kill $PID 2>/dev/null || true
else
  echo "⚠ config.yaml not found, skipping runtime tests"
  echo "✓ Build verified"
fi

echo "=== Verify Complete ==="
