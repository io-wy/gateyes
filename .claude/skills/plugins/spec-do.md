---
name: spec-do
version: 1.0.0
description: 规格实现。读取 `.kiro/specs/` 下的 spec，选取下一个未完成任务，实现代码并写回 Evidence。支持 L1/L2/L3 三级，自动检查前置条件、偏差记录、状态流转。Triggered when implementing from a spec, or user says "spec-do/实现规格/do spec/run spec/跑任务".
argument-hint: <feature-slug> [T-*|task description|"next"]
---
# Spec Do — Implement and Write Back

Goal: turn spec into working code, one slice at a time, keeping the spec alive as ground truth.

## Role
- The only skill that writes implementation code.
- Consumes spec artifacts. Updates them with evidence.
- Follows `SCHEMA.md` for structure and rules.

## Argument resolution

Slugify the argument and check if `.kiro/specs/*-<slugified>/` exists (date-prefixed match). If exactly one match, use it. If multiple matches, present all and ask the user to specify. If no match, report "spec not found" and suggest `/spec <feature>`.

If the user specifies a `T-*` that does not exist in the spec, report the error ("T-N not found in task list") and fall through to automatic selection (rules 2-5).

## Preconditions
- Spec exists under `.kiro/specs/<slug>/`.
- Status is `draft` or `active`.
- At least one unchecked item exists (T-* tasks, AC-* criteria, or "Done when" items at L1).
- For L3 specs in `draft` status: before the first implementation pass, run `/spec-check <slug> --lint`. If the verdict is `blocked`, do not proceed — report the issues. If `issues`, list them as warnings and proceed. If `clean`, proceed.

If preconditions fail, tell the user what's missing and suggest `/spec <slug>` to create or fix.
If status is `done`, suggest `/spec <slug>` to reopen first.

## Slice selection

Priority order:
1. User-specified `T-*` or task description.
2. At L3: consult the Dependencies section in `tasks.md` — respect ordering constraints. Pick the next unblocked, unchecked task.
3. At L2: next unchecked `T-*` in document order.
4. At L1: next unchecked "Done when" item.
5. If all tasks are checked but unchecked AC-* remain (L2/L3), the unverified AC becomes the slice. You must add a new task `T-N: [ ] Satisfy AC-X [discovered]` to the task list, implement it, then check off both the new T-* and the AC-*. Record in Deviations: `[discovered] T-N added for AC-X`.

You must choose the smallest self-contained slice that produces a testable outcome.

## Execution loop

For the selected slice:

```
1. Read    — spec + relevant source files (Grep/Glob to find them)
2. Plan    — state what you'll change and why (2-3 sentences, not a doc)
3. Code    — make the changes
4. Verify  — confirm the change works (see Verification below)
5. Write back — update the spec's Evidence section and check off tasks
```

Steps 2-5 happen in one pass. No pause between them unless verification fails.

## Verification

Concrete heuristics — you must not skip this step:
- If the project has a test runner configured (pytest, vitest, jest, go test, etc.), you must run it.
- If the changed files have corresponding test files, you must run those specifically.
- If lint/type-check is configured (ruff, eslint, tsc, etc.), you must run it on changed files.
- If none of the above exist, you must verify by reading the changed code and confirming it matches the spec's expected behavior. You must state what you checked.
- At L1: verify against "Done when" items. At L2/L3: verify against the relevant AC-*.

## Write-back rules

### On the spec file(s):
- Check off completed items: `- [x]`
  - L1: "Done when" items
  - L2/L3: `T-*` tasks and `AC-*` criteria
- Update `## Evidence` per SCHEMA.md §6 (L1 has 3 fields, L2/L3 has 5 fields).

### On frontmatter:
- Set `status: active` on first implementation pass.
- Set `status: done` only when all tasks and AC (or "Done when" at L1) are checked.
- Update `updated: YYYY-MM-DD`.

### On README.md index:
- Sync per SCHEMA.md §7 when status changes.

### On L3 Exit Criteria:
- If `tasks.md` has an `## Exit Criteria` section, check off its items as they become true.

## Deviation handling

If during implementation you discover the spec is wrong or incomplete:

| Situation | Action |
|---|---|
| Spec is slightly off (naming, minor detail) | Fix the spec inline, note in Deviations |
| Spec is missing a requirement | Add it to the spec, mark as `[discovered]`, implement |
| Spec conflicts with existing code | Stop, describe the conflict, ask user |
| Spec premise is invalid or requirements are based on incorrect assumptions | Stop, explain the issue, suggest `/spec <slug>` to revise |
| Scope is growing beyond the level | Suggest level escalation via `/spec <slug> --level <next level>` |

This is the bidirectional contract: the spec stays accurate because the implementer updates it.

## L1 Patch specifics
- Read `brief.md`, implement the fix, check off "Done when" items, fill Evidence.
- No IDs to track. Just do it.

## L2 Feature specifics
- Work through `spec.md` tasks in order.
- Each T-* has inline "done when" — verify that condition, then check it off.

## L3 Epic specifics
- Read `tasks.md` Scope section for slice boundaries and constraints.
- Consult Dependencies section to determine execution order.
- Consult Execution Notes for entrypoints, out-of-scope boundaries, and rollback strategy.
- Work through tasks respecting dependency constraints, not just document order.
- Cross-reference `design.md` for DD-* decisions (including Affected Modules for code locations).
- Cross-reference `requirements.md` for FR-*/AC-* verification.
- Check off T-* items in `tasks.md` and AC-* items in `requirements.md`. Both files must be updated on each pass.
- Update Evidence in `tasks.md`.
- Check off Exit Criteria items as they become true.
- On status change: update frontmatter in all three files per SCHEMA.md §4 (L3 frontmatter sync).

## Output format

### L2/L3
```text
Spec-do: <slug> / <slice>

Executed:
- what was done (1-2 sentences)
- files changed: list

Verification:
- what was checked, result

Write-back:
- tasks checked: T-1, T-3
- AC verified: AC-1
- deviations: none | summary
- debt: none | items

Status: active | done
Remaining: N tasks, M AC unchecked

Next step:
- `/spec-do <slug>` for next slice, or `/spec-check <slug>` to review
```

### L1
```text
Spec-do: <slug>

Executed:
- what was done (1-2 sentences)
- files changed: list

Verification:
- what was checked, result

Write-back:
- done-when checked: item descriptions
- deviations: none | summary

Status: active | done
Remaining: N items unchecked

Next step:
- `/spec-do <slug>` for next item, or done
```

## Hard rules
- Never implement without reading the spec first.
- Never skip write-back. Every implementation pass must update Evidence.
- Never mark `status: done` with unchecked AC items (L2/L3) or unchecked "Done when" items (L1).
- Never silently deviate from spec — always record it.
- If verification fails, fix or stop. Do not check off a failing task.
