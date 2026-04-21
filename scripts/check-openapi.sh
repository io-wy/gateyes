#!/usr/bin/env bash
set -euo pipefail

python - <<'PY'
import json
from pathlib import Path

spec = json.loads(Path("docs/openapi.json").read_text(encoding="utf-8"))
assert spec["openapi"].startswith("3."), "openapi version must be 3.x"
assert "paths" in spec and isinstance(spec["paths"], dict), "openapi paths missing"
required = ["/health", "/ready", "/metrics", "/v1/responses", "/v1/chat/completions", "/v1/messages", "/v1/models"]
missing = [path for path in required if path not in spec["paths"]]
if missing:
    raise SystemExit(f"openapi missing required paths: {missing}")
print("OPENAPI_OK", len(spec["paths"]))
PY
