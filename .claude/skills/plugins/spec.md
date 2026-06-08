---
name: spec
version: 1.0.0
description: 规格管理入口。创建、恢复、升级、放弃 specs，自动检测复杂度级别（L1 Patch / L2 Feature / L3 Epic），L3 拆分为 requirements→design→tasks 三文件。Triggered when starting a new feature spec, managing existing specs, or user says "写规格/spec/new spec/创建规格/spec list".
argument-hint: <feature-or-slug> [--level L1|L2|L3] [--drop] [--list]
---
# Spec — Unified Entry Point

Goal: assess complexity, create the right spec artifacts, and get to implementation fast.

## Role
- Single entry point for all spec work. No routing skills needed.
- Creates and manages spec artifacts. Does not implement code.
- Follows `SCHEMA.md` for structure and rules.

## Argument resolution

If no argument and no flag is provided, show a usage hint: `/spec <feature description>` to create or resume, `/spec --list` to see all specs. Do not proceed with an empty description.

The argument can be a feature description or an existing slug:
1. Slugify the argument (lowercase, kebab-case).
2. Check if `.kiro/specs/*-<slugified>/` exists (date-prefixed match).
3. If exactly one match → resume mode.
4. If multiple matches → present all matches and ask the user to specify.
5. If no match → create mode with the argument as feature description.

Slug date prefix: always use today's date (`YYYYMMDD`) when creating a new spec.

Special flags (`--list`, `--drop`, `--level` are mutually exclusive — if more than one is passed, reject with "These flags cannot be combined. Use one at a time."):
- `--list`: list all specs from README.md index. No other argument needed.
- `--drop`: mark an existing spec as `dropped`. Requires a slug.
- `--level L1|L2|L3`: override complexity. On new specs, skip assessment. On existing specs, trigger level escalation.

## List mode (`--list`)

Read `.kiro/specs/README.md` and present the index table. If no README exists, report "no specs found."

## Drop mode (`--drop`)

1. Read the spec and its Evidence section.
2. If status is `active`, warn the user: "This spec has implementation in progress. Dropping it will not revert code changes. Consider committing current work first." Include Evidence contents in the confirmation prompt.
3. Confirm with user: "Drop <feature>? This marks it abandoned."
4. Set `status: dropped` in frontmatter (for L3, update all three files per SCHEMA.md §4).
5. Add a line to Evidence: `- **Dropped**: YYYY-MM-DD — <reason from user or "user requested">`.
6. Sync README.md index.

## Resume mode

If `.kiro/specs/<slug>/` already exists:
1. Read the existing spec file(s) and their `## Evidence` section.
2. Report current status and what remains.
3. Based on current status:
   - `draft`: ask — continue refining, escalate level, or start implementation?
   - `active`: suggest `/spec-do <slug>` to continue implementation.
   - `done`: ask — reopen (set back to `active`) or leave as is?
   - `dropped`: ask — resurrect (set back to `draft`) or leave as is?
4. If `--level` is passed and differs from current level:
   - Upward (L1→L2, L1→L3, L2→L3): trigger level escalation per SCHEMA.md §1.
   - Downward (L2→L1, L3→L2, L3→L1): not supported. Explain to the user and suggest `/spec <slug> --drop` followed by creating a new spec at the desired level.

## Complexity assessment

When creating a new spec, assess before generating:

| Signal | Points toward |
|---|---|
| "fix", "bug", "typo", "config", "update" | L1 Patch |
| ≤2 files likely changed | L1 Patch |
| Single module, clear scope, 3-8 files | L2 Feature |
| Cross-module, new API surface, schema change | L3 Epic |
| Breaking change, new architecture, >8 files | L3 Epic |
| User explicitly passes `--level` | Use that level |

To improve assessment accuracy, you must briefly explore the codebase (Glob/Grep relevant paths) before deciding. When in doubt, you must start at the lower level. State your assessment and let the user confirm or override before generating.

## L1 Patch — `brief.md`

Generate using `docs/docs-ref/templates/brief.template.md`. Populate frontmatter per SCHEMA.md §4, Evidence per SCHEMA.md §6.

Sections: Problem (one paragraph), Fix (name files and approach), Done when (2-4 observable outcomes).

No IDs, no traceability, no ceremony.

## L2 Feature — `spec.md`

Generate using `docs/docs-ref/templates/spec.template.md`. Populate frontmatter per SCHEMA.md §4, Evidence per SCHEMA.md §6.

Single file with all three concerns: Context/Goals/Non-goals → Requirements (FR-*/AC-*) → Design (DD-*) → Tasks (T-* with inline "done when"). No separate T-V-* — validation is inline.

## L3 Epic — three files

Generate `requirements.md`, `design.md`, `tasks.md` using the templates in `docs/docs-ref/templates/`. Populate frontmatter per SCHEMA.md §4, Evidence per SCHEMA.md §6.

Workflow — each artifact is a separate invocation or explicit user "go":
1. Write `requirements.md`. Output it and ask: "Requirements look good? Say 'go' to proceed to design, or tell me what to revise."
2. On "go": write `design.md` referencing requirements. Ask again.
3. On "go": write `tasks.md` referencing design decisions. Ask again.

If the user runs `/spec <slug>` again after step 1, detect that `requirements.md` exists but `design.md` doesn't, and continue from step 2. Same logic for step 3.

## After spec creation

Output:
```text
Spec created:
- feature / slug / level
- path: .kiro/specs/<slug>/
- artifacts: brief.md | spec.md | requirements.md [+ design.md] [+ tasks.md]
- status: draft

Next step:
- Review and refine, or `/spec-do <slug>` to implement
```

Update `.kiro/specs/README.md` index per SCHEMA.md §7.

## Hard rules
- Never generate implementation code in this skill.
- Never skip user confirmation of complexity level.
- Never create L3 artifacts for L1/L2 work.
- If scope grows mid-spec, suggest level escalation explicitly.
- Level escalation must follow SCHEMA.md §1 (migrate content, delete old file, preserve evidence).
- On `--drop`, always confirm with user before setting `dropped`.


---

# Spec Schema — Shared Contract

> Single source of truth for the spec workflow.
> All skills must follow this file.

## 1. Complexity levels

| Level | Name | Trigger | Artifacts |
|---|---|---|---|
| L1 | Patch | bugfix, typo, config, ≤2 files | `brief.md` |
| L2 | Feature | single-module feature, small refactor, 3-8 files | `spec.md` |
| L3 | Epic | cross-module, new architecture, breaking change, >8 files | `requirements.md` + `design.md` + `tasks.md` |

Level is suggested by `/spec`, confirmed by user. When in doubt, you must start at the lower level — escalate if scope grows.

### Level escalation

When scope grows beyond the current level mid-work:
1. Create the new-level artifact(s) in the same directory, migrating content from the old file.
2. Delete the old-level file (`brief.md` or `spec.md`) after migration is complete.
3. Update frontmatter `level` in all new files.
4. Preserve any existing Evidence — copy it into the new artifact's Evidence section.
   - L1→L2/L3: copy `Changed`, `Tested`, `Deviations` verbatim. Set `Verified` to "migrated from L1 — re-verify against AC-*". Set `Debt` to "none".
   - L2→L3: copy all 5 fields verbatim into `tasks.md` Evidence.
5. Map L1 "Done when" items to AC-* in the new artifact where applicable. Items that don't map cleanly become Non-goals or are dropped.
6. Update README.md index.

Only one level's artifact set may exist at a time. Mixed sets (e.g., `brief.md` + `spec.md`) are forbidden.

## 2. Directory layout

```text
.kiro/specs/
├── README.md                    # index (auto-synced)
└── <YYYYMMDD-feature-slug>/
    ├── brief.md                 # L1 only
    ├── spec.md                  # L2 only
    ├── requirements.md          # L3 only
    ├── design.md                # L3 only
    └── tasks.md                 # L3 only
```

Slug format: `YYYYMMDD-<kebab-slug>` (date prefix, no hour).

## 3. IDs

| Prefix | Scope | L1 | L2 | L3 |
|---|---|---|---|---|
| `FR-*` | Functional requirement | — | spec.md | requirements.md |
| `AC-*` | Acceptance criterion | — | spec.md | requirements.md |
| `DD-*` | Design decision | — | spec.md | design.md |
| `T-*` | Task | — | spec.md | tasks.md |

### Rules
- L1: no IDs. "Done when" items serve as both tasks and acceptance criteria.
- L2: all IDs live in `spec.md`. FR/AC in Requirements section, DD in Design section, T in Tasks section.
- L3: IDs split across three files. Full traceability: FR→AC, FR→DD, DD→T.
- No `ADR-*`. `NFR-*` is folded into `FR-*` with a `[non-functional]` tag.
- No `T-V-*` — validation is part of each `T-*` (every task states its own "done when").

## 4. Frontmatter (all levels)

```yaml
---
feature: <name>
slug: <slug>
level: L1 | L2 | L3
status: draft | active | done | dropped
owner: <owner>
updated: YYYY-MM-DD
---
```

- `owner`: the person or agent responsible. Use a short identifier (e.g., GitHub username, "claude", "user"). Single-owner only — no shared ownership.

Four statuses only. No separate "gate results" — the gate is implicit in the transition between phases.

### L3 frontmatter sync
For L3 specs, `tasks.md` frontmatter is authoritative for `status` and `updated`. When any skill changes status or updated date, it must update all three files (`requirements.md`, `design.md`, `tasks.md`). `/spec-check` reads status from `tasks.md` when files disagree.

## 5. Lifecycle

```
draft  →  active  →  done
  │         ↑          │
  │         └──────────┘  (reopen)
  └──→ dropped    ←── any status
```

- `draft`: spec is being written or refined.
- `active`: implementation in progress.
- `done`: implementation complete, evidence recorded.
- `dropped`: abandoned or superseded.

### Transitions
- `draft → active`: first `/spec-do` pass sets this automatically.
- `active → done`: `/spec-do` sets this when all tasks and AC are checked.
- `done → active`: reopening — user explicitly requests via `/spec <slug>`, agent sets status back to `active`.
- `any → dropped`: user explicitly requests via `/spec <slug> --drop`. Agent sets status and records reason in Evidence.
- `dropped → draft`: user explicitly requests via `/spec <slug>` on a dropped spec. Agent confirms before resurrecting.

No phase-specific statuses. No gate ceremonies. The spec author (human or AI) decides when to move forward.

## 6. Evidence (write-back)

Every spec must have an `## Evidence` section updated during/after implementation.

### L1 (brief.md)
```markdown
## Evidence
- **Changed**: files modified (brief description)
- **Tested**: what was run, result
- **Deviations**: any divergence from spec (or "none")
```

### L2 (spec.md) and L3 (tasks.md)
```markdown
## Evidence
- **Changed**: file1.py, file2.vue (brief description)
- **Tested**: what was run, result
- **Verified**: which AC-* are met
- **Deviations**: any divergence from spec (or "none")
- **Debt**: follow-up items (or "none")
```

L1 omits Verified (no AC-*) and Debt (scope too small to track).

This is the bidirectional contract: the agent updates the spec as it implements, not after.

## 7. README index format

`.kiro/specs/README.md` is the spec index. Row format is a fixed contract:

```markdown
| Feature | Slug | Level | Status | Updated | Notes |
|---|---|---|---|---|---|
| Fix login timeout | 20260314-fix-login-timeout | L1 | active | 2026-03-14 | — |
```

- `Feature`: human-readable name.
- `Slug`: matches directory name exactly.
- `Level`: L1, L2, or L3.
- `Status`: lifecycle status only (draft/active/done/dropped).
- `Updated`: YYYY-MM-DD from frontmatter.
- `Notes`: free text — blockers, progress summary, or "—" if none.

Skills that create or change specs must sync this index.

## 8. Anti-patterns (forbidden)

- Gate result as frontmatter status (`design-ready`, `implementation-ready`)
- Destructive rollback commands in specs (`git reset --hard`, `git clean -f`)
- Method signatures or class names in requirements
- Scope exclusions written as requirements (use Non-goals instead)
- Stale Recovery sections (replaced by Evidence write-back)
- Rubber-stamp gate checks (replaced by `/spec-check` on demand)

## 9. Known limitations

- **Cross-spec file collision**: No mechanism detects when two active specs modify the same file. When running `/spec-do` on a spec whose changed files overlap with another `active` spec, the agent must warn the user about potential conflicts.
- **Single-owner workflow**: Designed for one developer + AI. Multi-person handoff (e.g., "I wrote the spec, you implement it") is not formally supported. The `owner` field is informational only.
- **No archival**: Old `done` or `dropped` specs accumulate in `.kiro/specs/`. Clean up manually when needed.


---

# Templates

## brief.template.md

---
feature: <feature-name>
slug: <YYYYMMDD-feature-slug>
level: L1
status: draft
owner: <owner>
updated: YYYY-MM-DD
---
# <Feature Name>

## Problem
What's broken or needs changing, and why. One paragraph.

## Fix
Concrete description of the change. Name the files and the approach.

## Done when
> List items in execution order. Each item should be independently verifiable.

- [ ] Observable outcome 1
- [ ] Observable outcome 2

## Evidence
_(filled during implementation)_
- **Changed**:
- **Tested**:
- **Deviations**:

## spec.template.md

---
feature: <feature-name>
slug: <YYYYMMDD-feature-slug>
level: L2
status: draft
owner: <owner>
updated: YYYY-MM-DD
---
# <Feature Name>

## Context
Problem, user, pain, desired outcome. 3-5 sentences max.

## Goals
- Goal 1

## Non-goals
- Out-of-scope item 1

## Requirements
- FR-1: ...
- FR-2: ...
- FR-3 [non-functional]: ...

## Acceptance Criteria
- AC-1: [ ] ... (covers FR-1)
- AC-2: [ ] ... (covers FR-2)

## Design
Brief technical approach. Name the modules, data flow, key decisions.

- DD-1: <decision> — because <rationale> (covers FR-1, FR-2)

## Tasks
- T-1: [ ] ... (covers DD-1) — done when: <observable outcome>
- T-2: [ ] ... (covers FR-3) — done when: <observable outcome>

## Evidence
_(filled during implementation)_
- **Changed**:
- **Tested**:
- **Verified**:
- **Deviations**:
- **Debt**:


## requirements.template.md

---
feature: <feature-name>
slug: <YYYYMMDD-feature-slug>
level: L3
status: draft
owner: <owner>
updated: YYYY-MM-DD
---
# Requirements: <Feature Name>

## Context
- **Problem**:
- **User / actor**:
- **Pain**:
- **Desired outcome**:
- **Why now**:

## Goals
- Goal 1

## Non-goals
- Out-of-scope item 1

## Existing Behavior
> Optional. Include only if there is a meaningful baseline to document. Remove if not applicable.

Current state and baseline.

## User Stories
> Optional. Include only if user-facing behavior is central. Remove if not applicable.

- As a ..., I want ..., so that ...

## Requirements
- FR-1: ...
- FR-2: ...
- FR-3 [non-functional]: ...

## Acceptance Criteria
- AC-1: [ ] ... (covers FR-1)
- AC-2: [ ] ... (covers FR-2)

## Constraints
> Optional. Include only if there are confirmed scope boundaries, migration limits, or tech selections.
> Not requirements — concrete implementation decided in design.md.
> Remove if not applicable.

## Open Questions
### Blocking
- none

### Non-blocking
- item

## Gate Notes
> Not a formal gate. When requirements feel stable, run `/spec <slug>` to continue to design.
> Checklist for self-assessment:
> - Problem is clearly framed
> - Core FR-* and AC-* exist and are testable
> - No method signatures or class names in FR-*
> - No scope exclusions written as FR-* (use Non-goals or Constraints)
> - Blocking questions are resolved or explicitly deferred


## design.template.md

---
feature: <feature-name>
slug: <YYYYMMDD-feature-slug>
level: L3
status: draft
owner: <owner>
updated: YYYY-MM-DD
---
# Design: <Feature Name>

## Context
Link to requirements: `.kiro/specs/<slug>/requirements.md`
Key constraints and design drivers.

## Requirements Alignment

| FR / AC | Design Response | Verification Path |
|---|---|---|
| FR-1 | ... | ... |
| AC-1 | ... | ... |

## Options Considered
- **Option A**: ... — pros / cons
- **Option B**: ... — pros / cons
- **Chosen**: Option X — because ...

## Decisions
### DD-1: <title>
- **Covers**: FR-1, FR-2
- **Decision**:
- **Rationale**:
- **Trade-offs**:

## Affected Modules
> Include concrete code locations. Consulted by `/spec-do` for file discovery.

- Modules:
- Contracts / interfaces:
- Data flow:

## Risks
> Include mitigations for each risk. Reviewed by `/spec-check`.

- Risk 1 → mitigation

## Open Questions
### Blocking
- none

### Non-blocking
- item

## Gate Notes
> Not a formal gate. When design feels stable, run `/spec <slug>` to continue to tasks.
> Checklist for self-assessment:
> - All in-scope FR-* are covered in Requirements Alignment
> - Key DD-* are recorded with rationale
> - AC-* verification paths are concrete
> - No AC semantic drift from requirements
> - Blocking design questions are resolved or deferred


## tasks.template.md

---
feature: <feature-name>
slug: <YYYYMMDD-feature-slug>
level: L3
status: draft
owner: <owner>
updated: YYYY-MM-DD
---
# Tasks: <Feature Name>

## Scope
> Optional. Include only if the task set covers a specific slice of a larger epic. Remove if the scope is obvious.

- **Slice**: what this task set covers
- **Constraints**: time / risk / dependency limits

## Tasks
- T-1: [ ] ... (covers DD-1, FR-1) — done when: <observable outcome>
- T-2: [ ] ... (covers DD-2, FR-2) — done when: <observable outcome>
- T-3: [ ] ... (covers FR-3) — done when: <observable outcome>

## Dependencies
- **Sequential**: T-1 before T-2
- **Parallel**: T-2 and T-3 can run together

## Exit Criteria
- [ ] All in-scope FR-* covered by T-*
- [ ] All in-scope AC-* verifiable from tasks
- [ ] All in-scope DD-* reflected in tasks

## Execution Notes
> Consulted by `/spec-do` before implementation. Include actionable content.

- **Entrypoints**: where to start
- **Out-of-scope**: what not to touch
- **Rollback**: describe scope and decision points (no destructive commands)

## Evidence
_(filled during implementation)_
- **Changed**:
- **Tested**:
- **Verified**:
- **Deviations**:
- **Debt**: