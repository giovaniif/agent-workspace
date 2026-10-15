# ADR 0023: Review pane, turn snapshots and the diff cache

Status: accepted, 2026-09-30.

## Decision

- **Scopes** are a domain rule: `RangeFor(scope, facts)` says where each diff starts (`last_turn` → the newest turn ref, `uncommitted` → `HEAD`, `branch` → merge base with `origin/<default>`). Every scope ends at the working tree, untracked files included.
- **Working tree as a tree.** The git adapter copies the worktree's index to a temp file, runs `git add -A` and `git write-tree` against it (`GIT_INDEX_FILE`), and diffs `git diff <from> <tree>`. The real index and the worktree are never touched. Copying the index keeps git's stat cache, so only changed files are hashed.
- **Turn snapshots.** On every `UserPromptSubmit`, a worker commits that tree (`commit-tree`, author `crew`) and points `refs/crew/turns/<session>/<worktree key>/<n>` at it, then deletes the session's older turns in that worktree. The worktree key is 12 hex chars of the SHA-1 of its path, because worktrees of one repo share refs. When the scanner sees a worktree removed, it deletes every turn ref of that worktree from the main checkout.
- **Cache.** `app.Reviewer` keeps parsed diffs by `<resolved from commit>..<tree hash>`, 128 entries, FIFO. Reopening an unchanged review runs `write-tree` and `rev-parse` but no `git diff`.
- **Where it runs.** `review.open` builds on the connection goroutine, at most 4 worktrees at once; snapshots and ref cleanup run on workers. Nothing touches git on the loop. The TUI highlights and lays out a review in the command that fetched it, so `View` only paints.
- **Viewed marks** are `{worktree, path, blob}`, stored in SQLite (`viewed` table, migration 0004). A mark counts only while the file's blob (from the diff's `index` line) is the one that was viewed, so it resets as soon as the file changes again.
- **Layout.** `r` calls `client.review`, which resizes the sidebar pane to 75% of the window and re-points its `window-resized` hook at that width; closing puts back 48 columns. The sidebar collapses to a rail of glyphs and numbers.
- **Agent column (#95).** From 120 columns the review draws a column between the rail and the diff, as in `docs/images/review.png`: the session's name and state, its recent actions oldest first, its last message (`SessionCard.Said`, from the latest Stop), and a "Draft review → this session" box counting the draft's comments and worktrees. It is built from the events and drafts the TUI already holds. The key hints run under it, so they keep their width.
- **Highlighting** uses chroma's lexer engine with a curated set of its XML lexers copied into `internal/tui/syntax/` (MIT, `COPYING` kept) plus its Go rules, built on first use. Colors come from the theme, not chroma's styles.

## Why

- `git stash create`, the issue's suggestion, leaves untracked files out, so a file created before the prompt would show up as part of the turn. A tree from a temp index covers them and gives one code path for snapshots and diffs.
- chroma's `lexers` and `styles` packages build every lexer and style in `init`, about 7 ms per process; the binary is also `crew hook`, whose budget is 20 ms p95. With the curated set chroma's init is under 0.1 ms.
- Keeping one snapshot per session and worktree bounds the refs; the scope only needs the latest.

## Rejected

- **Snapshotting with `git stash create`:** misses untracked files (above).
- **Diffing in Go:** the architecture says diffs come from `git diff`.
- **Keeping all turns:** refs grow without bound and nothing reads older turns yet.
- **Persisting viewed marks in the TUI:** the TUI does no disk IO; the daemon owns state.

## Consequences

- A prompt's snapshot can land a few milliseconds after the agent starts editing on a large repo; that edit then counts as before the turn.
- A worktree with a huge untracked, non-ignored directory makes `git add -A` slow; it runs off the loop, but the review waits for it.
- nvim is a separate issue. Comments and hunk stage/revert came later, in ADR 0028.
