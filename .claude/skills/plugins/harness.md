---
name: harness
version: 1.0.0
description: 一键架构检查。跑 lint-deps + lint-quality + harness-audit，输出健康度评分和违规列表。
argument-hint: [--fix]
context: fork
allowed-tools: Bash(go run:*)
---

# Harness — Architecture Gate

Run the full harness check pipeline and report results.

## Execution

```bash
make lint-arch && make harness-audit
```

If `--fix` is passed, attempt to auto-fix violations (when possible) and re-run.

## Output

Report score (0-100) and list any violations with file:line locations.
