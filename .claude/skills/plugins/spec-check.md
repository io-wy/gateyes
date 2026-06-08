---
name: spec-check
version: 1.0.0
description: 规格校验。对 `.kiro/specs/` 下的 spec 执行状态检查、结构 lint（S-01~S-14）、质量 review（可追溯性/完整性/适配度），输出 clean/issues/blocked 判定。按需使用，非强制门禁。Triggered when reviewing a spec's quality, or user says "check spec/spec-check/检查规格/规格校验/lint spec".
argument-hint: <feature-slug> [--lint|--review|--status]
---
# Spec Check — Inspect, Lint, Review

Goal: one command for all quality checks. Optional, not a gate.

## Role
- Read-only inspector. Does not modify spec files.
- Combines the old spec-status, spec-lint, and spec-review into one skill.
- Follows `SCHEMA.md` for rules.

## Argument resolution

Slugify the argument and check if `.kiro/specs/*-<slugified>/` exists (date-prefixed match). If exactly one match, use it. If multiple matches, present all and ask the user to specify. If no match, say so and suggest `/spec <feature>`.

## Modes

Flags are mutually exclusive. If multiple flags are passed, ignore them and use smart mode (all three checks).

| Flag | What it does | Default |
|---|---|---|
| (no flag) | Smart mode: runs all three, reports concisely | yes |
| `--status` | Status only: where is this spec, what's next | |
| `--lint` | Lint only: mechanical rule checks | |
| `--review` | Review only: quality and completeness judgment | |

## Status check

Report:
- Feature name, slug, level, current status.
- Which artifacts exist.
- Task progress: N/M checked.
- AC progress: N/M verified.
- Evidence: present / partial / missing.
- Recommended next step.

## Lint checks

### All levels
- `S-01`: frontmatter `status` is one of: draft, active, done, dropped
- `S-02`: frontmatter `level` matches actual artifact set (L1→brief.md, L2→spec.md, L3→three files). For L3 in `draft` status, partial sets are acceptable (requirements.md alone, or requirements.md + design.md). Full three-file set is required only when status is `active` or `done`.
- `S-03`: slug matches directory name
- `S-04`: no destructive git commands in spec text
- `S-05`: README.md index entry exists and status matches

### L2+ only
- `S-06`: FR-* does not contain method signatures or class names
- `S-07`: AC-* must reference at least one FR-* (may also reference DD-* for traceability)
- `S-08`: every T-* has a "done when" condition
- `S-13`: Evidence section contains all required fields for the spec's level (L1: Changed/Tested/Deviations; L2/L3: Changed/Tested/Verified/Deviations/Debt)

### L3 only
- `S-09`: DD-* references FR-*
- `S-10`: T-* references DD-* or FR-*
- `S-11`: requirements.md does not reference DD-* or T-*
- `S-12`: design.md Requirements Alignment section exists
- `S-14`: All L3 frontmatter fields (`status`, `level`, `updated`) are consistent across requirements.md, design.md, and tasks.md. `tasks.md` is authoritative when files disagree.

## Review assessment

Evaluate (only when `--review` or default smart mode).

Review criteria are level-dependent:

### L1 review
- Is the problem clearly stated?
- Is the fix concrete (names files and approach)?
- Are "Done when" items observable and testable (not vague)?
- If status is active/done: is Evidence populated?

### L2+ review

#### Traceability
- Are all FR-* covered by design and tasks?
- Are all AC-* verifiable from the task list?
- Any orphan tasks not traced to requirements?

#### Completeness
- Is the problem clearly framed?
- Are acceptance criteria testable (not vague)?
- Is the design approach concrete enough to implement?
- Are tasks small enough to be atomic?
- Does every T-* have a "done when" condition?

#### Fit
- Does the design actually solve the stated requirements?
- Is there scope creep between requirements and tasks?
- Is the level appropriate for the actual complexity?

#### Evidence (if status is active or done)
- Is Evidence section populated?
- Do checked tasks have corresponding evidence?
- Are deviations recorded?

### L3 additional review

#### In `requirements.md`:
- Do Existing Behavior, User Stories, Constraints sections contain meaningful content? If empty or placeholder-only, recommend removal.

#### In `design.md`:
- Does Options Considered include at least 2 genuinely different approaches with substantive trade-offs?
- Do Affected Modules identify concrete code locations?
- Are Risks accompanied by mitigations?

#### In `tasks.md`:
- Does Dependencies section match actual task ordering needs?
- Are Exit Criteria items aligned with AC-* coverage?
- Do Scope and Execution Notes (Entrypoints, Out-of-scope, Rollback) contain actionable content? If empty, recommend removal.

## Verdicts

| Verdict | Meaning |
|---|---|
| `clean` | No issues found. Ready to proceed. |
| `issues` | Problems found but fixable. List them. |
| `blocked` | Critical problems. Cannot proceed without resolution. |

## Output format

```text
Spec check: <slug> / <mode>

Status:
- level / status / progress (tasks N/M, AC N/M)

Lint: (N passed, M failed)
| Rule | Note |
|---|---|
| S-06 | FAIL: FR-3 contains class name "UserService" |

Review:
- traceability: ok | gaps
- completeness: ok | issues
- fit: ok | concerns
- evidence: ok | partial | missing

Verdict: clean | issues | blocked
- summary (1-2 sentences)

Issues:
1. [S-06] FR-3 contains implementation detail → move to design
2. [review] AC-4 is not testable → rephrase

Next step:
- if draft/active: fix issues or `/spec-do <slug>` if clean
- if done: no action needed, or `/spec <slug>` to reopen
- if dropped: `/spec <slug>` to resurrect
```

Lint table shows only FAIL rows. PASS count is in the summary line.

## Hard rules
- Never modify spec files. Report only.
- Never block implementation — verdicts are advisory.
- Keep output concise. No multi-page reports.
- If spec doesn't exist, say so and suggest `/spec <feature>`.
- To list all specs, suggest `/spec --list`.
