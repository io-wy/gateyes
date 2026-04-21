#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <backup-file> <target-sqlite-file>" >&2
  exit 1
fi

backup="$1"
target="$2"
cp "$backup" "$target"
echo "database restored to: $target"
