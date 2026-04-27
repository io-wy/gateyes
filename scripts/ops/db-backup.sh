#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <sqlite-file> <backup-dir>" >&2
  exit 1
fi

src="$1"
dst_dir="$2"
mkdir -p "$dst_dir"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
dst="$dst_dir/$(basename "$src").$timestamp.bak"

cp "$src" "$dst"
echo "backup created: $dst"
