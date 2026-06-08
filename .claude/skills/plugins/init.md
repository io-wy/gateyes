---
name: init
version: 1.0.0
description: 展开项目骨架模板。按需生成 Dockerfile、CI/CD、文档、cmd/internal/ 目录。新项目第一步用这个。
argument-hint: [all|docker|cicd|docs|internal]
context: fork
allowed-tools: Bash(cp:*), Bash(mkdir:*)
---

# Init — Expand Project Skeleton

Expand templates from `docs/docs-ref/docs/docs-ref/templates/init/` into the project root.

## Usage

```
/init all      → make init-all
/init docker   → make init-docker
/init cicd     → make init-cicd
/init docs     → make init-docs
/init internal → make init-internal
```

## Execution

```bash
make init-{{target}}
```

If no argument, default to `all`.
