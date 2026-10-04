# internal/app

Use cases and the ports they need. Depends only on `domain`. Tests use in-memory fakes of the ports in `fakes_test.go` / `*_fake_test.go`; `gremlins` runs here in CI (≥ 80%).

- **Use cases:** `Sessions` (start and end), `ScanWorktrees`, `RefreshPRs`, `Cleanup` (plan and execute), `Reviewer`, `SnapshotTurn`/`DropTurns`, `ApplyHunk`, `SendPrompt`, `SendSwitches`, `DiscoverWorkspace`, `RefreshRepoFacts`, `ReconcilePanes`, `WorktreeSetup`, `DiskSizes`, `Transcripts` (`Page`, `Tail`, `TranscriptTail.Read`/`Since`).
- **Ports:** `TerminalHost`, `HarnessAdapter`, `Store`, `Notifier`, `Foreground`, `ProcessTable`, `WorkspaceFS`, `RepoInspector`, `WorktreeAdder`, `WorktreeLister`, `PRFinder`, `TitleResolver`, `ReviewGit`, `HunkGit`, `CleanupGit`, `WorktreeHolders`, `Trash`, `CleanupAudit`, `Sizer`, `Editor`, `Onboarder`, `TranscriptFiles`, `TranscriptWatcher`, `TranscriptParser`, and for setup recipes `RecipeSource`, `MainCheckouts`, `SetupFS`, `CommandRunner`. When a port changes, update the `-tags integration` fakes too.
- **Harnesses.** `HarnessAdapter` turns a `LaunchRequest` into the `PaneSpec` that runs the harness. The pane's `$TMUX_PANE` is how hooks find the session again.

## Cleanup execution

`app.Cleanup` plans, backs up `backup_then_ask` worktrees under `$AGENTWS_HOME/backups/<ts>/<name>/`, checks lsof and the git facts once more, renames `remove` worktrees into `$AGENTWS_HOME/trash/`, then runs `git worktree prune` per repo. The trash is emptied in the background, 4 at a time. Branches are never deleted. A detached HEAD with commits not in the default branch gets `backup/wt-<name>` first. `RemoveWorktree` serves the disk view's `d`/`b`. Any change here needs tests for the never-destroy cases in the root rules. See [ADR 0021](../../docs/adr/0021-worktree-cleanup.md).

## Disk sizes

`app.DiskSizes.Get` never waits: `du` runs in a pool of 2, cached for 5 min, and nothing on the loop, the render path or a request waits for it. See [ADR 0030](../../docs/adr/0030-worktrees-disk-view.md).
