#!/bin/bash
# Start Grafana for gateyes using project-local config.
# Assumes Prometheus is already scraping http://127.0.0.1:8028/metrics.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$(dirname "$SCRIPT_DIR")")"

GRAFANA_BIN="${GRAFANA_BIN:-/opt/homebrew/opt/grafana/bin/grafana}"
CONFIG="${SCRIPT_DIR}/grafana.ini"

mkdir -p /tmp/grafana-data /tmp/grafana-logs /tmp/grafana-plugins

exec "${GRAFANA_BIN}" server \
  --config "${CONFIG}" \
  --homepath /opt/homebrew/opt/grafana/share/grafana \
  --packaging=brew \
  cfg:default.paths.data=/tmp/grafana-data \
  cfg:default.paths.logs=/tmp/grafana-logs \
  cfg:default.paths.plugins=/tmp/grafana-plugins
