# ADR 0041: Scoped AGENTS.md files

Status: accepted, 2026-10-01.

## Decision

- The root `AGENTS.md` keeps only repo-wide guidance: what the project is, commands that apply everywhere, layer, TDD and comment rules, the PR workflow, repo hygiene, and an index of scoped files. It stays under 150 lines.
- Feature notes (test commands, by-hand recipes, subsystem design) live in an `AGENTS.md` in the directory most edits for that feature start in: `cmd/crew`, `internal/{domain,app,daemon,rpc,tui}`, `internal/adapters` and its `git`, `tmux`, `setup` and `onboard` subdirs, `nvim`, `scripts`, `test/e2e` and `test/integration`.
- Each scoped `AGENTS.md` has a `CLAUDE.md` symlink to it beside it, as the root does: Codex reads `AGENTS.md`, Claude Code reads `CLAUDE.md`, and both load a subdirectory's file when working there.
- `ARCHITECTURE.md` keeps the stack, layers, process model and data flow, the fast-path rules and the budgets table, and links to the scoped files for subsystem detail.
- `scripts/lint-agents`, in `make lint`, fails when the root is over the limit, a scoped `AGENTS.md` has no `CLAUDE.md` symlink, or a relative link in an `AGENTS.md` or `ARCHITECTURE.md` does not resolve.

## Why

- Every session loaded the whole root file, most of it about features it was not touching. Scoped files load only where they apply.
- A size limit and a check keep the root from growing back; the symlink check keeps Claude Code and Codex seeing the same text.

## Limits

- A feature that spans layers has one home; the other directories link to it rather than repeat it.
- The line limit counts lines, not words, so long bullets still need judgement.
