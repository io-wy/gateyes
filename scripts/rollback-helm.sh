#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <release-name> <revision> [namespace]" >&2
  exit 1
fi

release="$1"
revision="$2"
namespace="${3:-gateyes}"

helm rollback "$release" "$revision" -n "$namespace"
