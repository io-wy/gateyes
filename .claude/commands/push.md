---
description: Commit and push a focused Gateyes module change
argument-hint: <module>
---

# /push $ARGUMENTS

You are preparing to commit and push one focused Gateyes module. Treat `$ARGUMENTS` as the module or scope name.

## Intent

Commit and push only the changes that belong to the requested module, for example:

- `/push route`
- `/push cache`
- `/push batch`
- `/push frontend`

## Workflow

1. Inspect the repository state with `git status --short --branch`.
2. Identify files related to `$ARGUMENTS`.
3. Do not stage unrelated files. If a dirty file is ambiguous, leave it unstaged and mention it.
4. Never stage likely secrets or accidental files, especially filenames or contents containing passwords, API keys, DSNs, tokens, or local credentials.
5. Review the candidate diff with `git diff -- <files>` before staging.
6. Stage only the selected files.
7. Run `git diff --cached --check`.
8. Run focused validation for the module:
   - Backend Go module: `GOCACHE=$PWD/.gocache go test ./...`
   - Web module: `cd web && npm run lint && npm run build`
   - Helm/deploy module: run available chart/template validation if the project exposes one; otherwise inspect the rendered diff carefully.
9. Commit with a concise conventional message that names the module, such as `Update route handling`.
10. Push the current branch with `git push`.
11. Finish with the commit hash, pushed branch, validation commands, and any dirty files intentionally left behind.

## Guardrails

- If `$ARGUMENTS` is empty, stop and ask for the module name.
- If the branch has no upstream, set it with `git push -u origin HEAD` after confirming the target branch is correct.
- If validation fails, do not commit or push. Explain the failure and the smallest next fix.
- If unrelated staged files already exist, do not commit them blindly. Unstage or ask before proceeding.
- Do not run destructive cleanup commands such as `git reset --hard`, `git clean`, or `rm` unless explicitly requested.
