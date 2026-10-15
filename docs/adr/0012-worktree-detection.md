# ADR 0012: Worktree detection and attribution

Status: accepted, 2026-09-29.

## Decision

- Two triggers feed one scanner goroutine: a `PostToolUse` hook whose command runs `git worktree add` (or a hook reporting a new cwd) wakes it at once, and a 10 s poll catches everything else. Both run the same full scan, so there is one code path for adding, updating and dropping worktrees.
- The scan targets every registered repo plus every session's last hook `cwd`, so a worktree in a repo that is not in any workspace is still found. Listings are keyed by main checkout.
- Attribution uses what hooks already carry: `cwd` and the Bash command. A claim lasts 30 s. With claims from several sessions, the one whose command names the new path or branch wins; with no winner the worktree stays unassigned rather than guessed.
- The first scan of a repo in a daemon's life adopts unknown worktrees as unassigned. `worktree.assign` (and `crew worktree assign`) fixes ownership by hand.
- Ownership is stored on the worktree (`SessionID`) and mirrored in `Session.WorktreeIDs`; the daemon updates both in the same loop turn.
- PRs come from one `gh pr list --state all` per repo every 60 s, not one call per worktree.
- Removal is a real delete (`Store.DeleteWorktree`, `removed_worktree` diff), only for repos the scan could read.
- Integration tests that drive the daemon with real adapters live in `test/integration`, because depguard bars `internal/daemon` (tests included) from `os/exec`.

## Why

- Hooks give the owning session directly and within milliseconds; polling alone would need 10 s and a guess.
- A full rescan on each trigger is cheap (one `git worktree list` per repo) and avoids parsing `git worktree add` arguments to find the repo and path.

## Rejected

- **Pane cwd from tmux:** the hook payload already has the harness's cwd, and asking tmux would add an exec per session per poll.
- **fsnotify on `.git/worktrees`:** a second mechanism for the same data; the hook covers the latency case.
- **Adopting on first scan by timestamps** (crediting worktrees made after a claim): needs a stat per worktree and still guesses.
