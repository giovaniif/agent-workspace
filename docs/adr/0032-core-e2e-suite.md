# ADR 0032: Core e2e suite

Status: accepted, 2026-09-30.

## Decision

- `test/e2e` runs the real binary against a temp `CREW_HOME`, temp repos and its own `tmux -L` server per script. Five scripts cover the core loop: hook events drive session state, agent-created worktrees attach, a merged PR's worktree is removed with its branch kept, a dirty merged worktree is backed up, and review comments reach the agent as the documented prompt.
- Fake `claude`, `codex` and `gh` live in `test/e2e/testdata/bin` as shell scripts, not `test/e2e/bin` as the issue said: `scripts/tdd-check` copies only test files and `testdata/` onto the base checkout, and without the fakes there the base run would launch whatever real `claude` is on `PATH`. The agent fakes replay a steps file and call `crew hook` exactly as the real harnesses' hook config does, so the hook path under test is the real one.
- No fixed sleeps. Scripts poll with `eventually` (15 s deadline). Hooks are fire-and-forget on separate connections and can reach the daemon out of order, so where order matters the script waits for a state and then opens a `gate` that lets the fake go on.
- The daemon takes two test hooks from the environment, read once in `daemon.Run`: `CREW_TEST_CLOCK` shifts the cleanup clock (so the 4 h activity grace passes without waiting) and `CREW_TEST_PR_POLL` replaces the 60 s PR poll. Unset, nothing changes. The worktree poll is left alone.
- `crew review send` was added so the review scenario runs from the CLI.
- The suite found a race: a worktree scan kicked by an earlier hook can see a new worktree while `git worktree add` runs, before the `PostToolUse` claim arrives, and it stayed unassigned. `domain.ReclaimWorktrees` now attaches an unassigned worktree once a recent claim from one session names its path or branch.

## Why

- Env hooks keep the daemon's wiring the same one users run; a clock port through the RPC would be more surface for a test-only need.
- Gates make ordering explicit instead of hoping two processes' hooks land in the order they were sent.

## Limits

- The suite needs `tmux`; CI installs it before `go test ./...`.
- A reclaim takes only claims made inside the worktree's repo (from the hook's cwd) that name it by a whole word: its branch, or a path ending in its dir name. Claims from two sessions naming the same worktree leave it unassigned. First-seen attribution still matches by substring.
