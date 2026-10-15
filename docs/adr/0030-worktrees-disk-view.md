# ADR 0030: Worktrees and disk view

Status: accepted, 2026-09-30.

## Decision

- `w` opens the view, widening the sidebar pane the way the review does. `esc` or `w` closes it. It needs the cleanup engine, so it is off without `WithCleanup`.
- `disk.view` answers one `rpc.DiskView`: volume free and total, the auto-cleanup interval, the shared deps store (path and size), one `domain.DiskRow` per worktree (size, the engine's decision and its reason) and the last 8 audit log lines. The rows are `Cleanup.Plan`, so the view shows what cleanup would do and never does it. It runs on the connection goroutine, under the same mutex as cleanup runs.
- Sizes come from `app.DiskSizes`, a cache in front of the `Sizer` port. `Get` never waits: it returns the cached size and starts a background `du` for a path that is new or older than 5 min, at most 2 at a time. A size not known yet is `domain.SizePending` and the view shows `…`. The TUI refetches every 2 s while any size is pending and every 10 s otherwise, and only while the view is open. `Forget` drops a size when a worktree is removed.
- `fs.Du` runs `du -sk -P`. One run counts a hardlinked file once, so a worktree whose dependencies are hardlinked is not charged twice within itself. Symlinks are not followed.
- `domain.Reclaimable` sums the sizes of rows whose decision is `remove` or `backup_then_ask`; `domain.TotalSize` sums all. Both leave pending sizes out and report how many there were, and the header then shows the total with a `+`.
- The deps store is `$CREW_DEPS_STORE`, else pnpm's default store if it exists (`~/Library/pnpm/store`, `~/.local/share/pnpm/store`). Without one the header leaves it out.
- The header is three panels, as in `docs/images/worktrees.png`: a disk bar split into kept worktrees, reclaimable ones, other used space and free space with its legend; the auto-cleanup rules (or "off"); and the deps store. They sit side by side from 140 columns and stack below. The state column is `domain.WorktreeStatus` (`◐ in use`, `○ idle`, `✓ merged`, `! merged, dirty`, `! detached`), from the owner session and the decision, and the session column names the harness (`1 claude`); #94.
- `d` and `b` call `cleanup.worktree {path, backup}` after a `y/n` confirmation, and go through `app.Cleanup.RemoveWorktree`. Without `backup` it removes only what `Execute` would (merged and clean); a dirty or detached worktree is refused, and the TUI says to press `b`. With `backup` a `backup_then_ask` worktree is backed up (and gets its branch when detached), then goes through the same last checks as `Execute`: lsof again, git facts again with the fingerprint taken at backup, session and grace. A failed backup or branch, a holder, a changed fingerprint or a kept decision leaves it in place, and the outcome says so. A `keep` worktree is never touched. Every action is audited.
- On success the daemon drops the worktree from state at once (`WorktreeRemoved`) and the TUI drops the row, so it updates without waiting for the next scan. The TUI now also applies `removed_worktree` diffs, which it ignored before.
- `k` asks and sends `ports.kill` for the row's process groups, `g` selects and focuses the owning session, and `o` calls `shell.toggle` with just the worktree (see [0029](0029-shell-and-nvim.md)): the same kept-alive shell `t` uses, below the owning session's agent pane, or below the main slot for an unowned worktree. There is no separate `worktree.shell`; it was a second path that opened a fresh `$SHELL` pane each time and swapped it into the main slot.

## Why

- The engine already decides what is safe. Routing the view's actions through it, instead of a second delete path, keeps one place that can lose work.
- `du` on a 1 GB `node_modules` takes seconds. Opening the view has to cost one RPC of `git status` per worktree at most, so sizes fill in behind it.
- Reading the plan for the rows means the reclaimable total and the cleanup decisions cannot disagree.

## Limits

- `du` counts an APFS clone (`deps = clone`) in full, so a cloned worktree looks as big as its source and the reclaimable total overstates what deleting it frees. Hardlinks shared between worktrees or with the deps store are counted in each worktree's size.
- A size whose `du` fails (path gone, permission) stays `…` until the next attempt.
- Recent cleanups show at most 5 lines and come from `cleanup.log`, so they include actions from the CLI and from automatic runs.
- Sizes are cached per daemon life; the first view after a restart shows `…` until `du` catches up.

## Rejected

- **Computing sizes in the TUI process:** the TUI must not exec or touch disk while rendering, and several TUIs would repeat the work.
- **`git count-objects` or `git ls-files` sizes:** they miss untracked and ignored files, which are most of the bytes.
- **A `d` that backs up dirty worktrees first:** it hides the fact that uncommitted work is about to leave the worktree. `b` says it.
