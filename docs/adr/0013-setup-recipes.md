# ADR 0013: Setup recipes and shared dependencies

Status: accepted, 2026-09-29.

## Decision

- A repo's recipe is the `[setup]` table of `<repo>/.crew.toml`, read from the main checkout (so uncommitted edits count): `copy` and `link` (paths relative to the repo, same path in the worktree), `run` (shell commands, run in the worktree), `deps` (`clone`, `link` or `install`). Unknown keys are an error, so a typo does not silently do nothing.
- Steps run in the order copy, link, deps, run, and stop at the first failure. Nothing already in the worktree is overwritten: a second run skips what exists.
- `deps = "clone"` copies the main checkout's `node_modules` with `cp -c -R` (APFS clonefile; reflink elsewhere), so it costs no disk until files change. It falls back to the package manager's frozen install when the worktree's lockfile differs from main's (name or content hash), or main has no `node_modules`, and logs which. A lockfile we do not know means no install and a logged skip.
- The rules (path validation, lockfile comparison, install command per lockfile) are pure functions in `domain`. `app.WorktreeSetup` sequences them behind ports; `internal/adapters/setup` holds the disk, TOML and command code; `git.Worktrees.MainCheckout` finds the main checkout through `git rev-parse --git-common-dir`.
- `crew setup-worktree <path>` runs the recipe in its own process and prints one line per step, then duration and the change in free bytes on the volume. It does not go through the daemon: it is slow, off every hot path, and needs no daemon state.
- Recipe paths must be relative and stay inside the repo; `run` commands are the repo owner's own code and run as written.

## Why

- Frozen installs are the only safe fallback: a cloned `node_modules` from another lockfile can be silently wrong.
- Free bytes on the volume is what `df` shows and needs no directory walk; a walk over a cloned tree would count shared blocks twice.
- Reading the recipe from main keeps one source of truth for every worktree of the repo.

## Not done yet

- Running it automatically when the tool creates a worktree, and from the worktree-detection hook: neither exists yet. Both call `app.WorktreeSetup.Run`.
- A workspace-level recipe: there is no workspace config file yet.

## Rejected

- **Hardlinking `node_modules`:** a write in one worktree changes every other one. Clones copy on write.
- **A recipe format with per-step options:** `copy`, `link`, `run` and `deps` cover the cases so far.
