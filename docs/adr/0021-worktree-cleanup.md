# ADR 0021: Worktree cleanup

Status: accepted, 2026-09-30.

## Decision

- `domain.PlanCleanup(worktree, facts, now)` decides each worktree alone. In order it keeps: the main checkout, anything whose facts could not be gathered, anything a process has its cwd in, a worktree whose session is live (not idle), anything active in the last four hours (`CleanupGrace`), and a worktree on the default branch. Then a detached HEAD not in `origin/<default>` is `backup_then_ask` with a `backup/wt-<name>` branch, an unmerged one is kept, a merged dirty one is `backup_then_ask`, and a merged clean one is `remove`. Merged means the PR is merged on GitHub (covers squash merges) or HEAD is reachable from `origin/<default>`. With no origin, nothing counts as merged by reachability.
- "In use" is one `lsof -d cwd` over all processes (`adapters/procs`). Pane shells, editors and dev servers all show up there, so tmux is not asked separately. If lsof fails, every worktree is kept.
- `app.Cleanup.Execute` plans afresh. Just before it moves anything, it runs a second lsof and asks git for the facts again. A worktree that someone entered, or whose status or diff changed, is kept. Nothing can stop a writer that starts after that last check. The window is milliseconds long, and the process checks make it unlikely.
- Removal is `rename` into `~/.crew/trash/`, then one `git worktree prune` per repo. A background pool deletes the trash, at most 4 deletions at once, and each cleanup run (never a dry run) empties leftovers from earlier runs. A rename that fails, for example across volumes, leaves the worktree in place and reports the failure. There is no slow fallback.
- Backups go under `~/.crew/backups/<ts>/<name>/`: `patch.diff` (`git diff HEAD --binary`, staged and unstaged), `untracked.tar` (untracked, non-ignored files, only when there are any) and `status.txt`. The same worktree state (a hash of `git status --porcelain=v2 --branch` plus `git diff HEAD --binary` when dirty) is backed up once per daemon life. Backup branches are made with `git branch` without `-f`, trying `-2`, `-3` and so on when the name is taken. Nothing deletes or moves a branch.
- `backup_then_ask` backs up and keeps the worktree. The ask is left to the TUI. Until then, the plan and the audit log show it.
- The daemon runs cleanup every 10 min and whenever the PR poll first sees a worktree's PR merged, on its own goroutine and never on the loop. `cleanup.plan` (`crew cleanup --dry-run`) and `cleanup.run` (`crew cleanup`) share a mutex with it.
- Every action that is not a keep is appended as one JSON line to `~/.crew/cleanup.log`.

## Why

- A worktree an agent just created from `main` is reachable from `origin/main` and clean, so it looks merged. The one-hour grace, plus the session and process checks, keep it.
- A rename is instant whatever the size. Measured: 50 worktrees holding 1 GB each return in about 1 s.

## Rejected

- **`git worktree remove`:** it deletes in the foreground, which is slow for large `node_modules`, and `--force` would drop dirty work.
- **Asking tmux for pane cwds:** lsof already sees the pane shells, and a second source could disagree.
- **Copying to the trash across volumes:** as slow as deleting. Such worktrees stay in place.
