#!/bin/bash
# Gateyes Loadgen — multi-scenario traffic generator
# Usage: ./run.sh
# Env: GATEWAY_URL (default: http://localhost:8028)

cd "$(dirname "$0")"

# Ensure dependencies
pip install -q -r requirements.txt

# Run
exec python -u main.py
