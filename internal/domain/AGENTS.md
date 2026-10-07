# internal/domain

Pure types and rules: no IO, no imports from other `internal/*` packages. `Workspace`, `Repo`, `Task`, `Session`, `Worktree`, `Harness`, `AgentState`, `Usage`, `ReviewDraft`, `Comment`, `CleanupPlan`.

## Tests

- Everything here is table-tested, with no mocks. Tests are in-package (`package domain`): depguard bans `domain` from importing `internal/...`, and that includes an external `domain_test` package importing `domain`.
- `gremlins` runs here in CI; the mutation score must stay ≥ 80%.
- The review prompt format is pinned by `testdata/review_prompt.golden`; change it only on purpose and update the golden by hand.

## Rules

- `Session.Apply(event) (Session, []Effect)`: the state machine. Adapters map hooks to harness-neutral events (`session_start`, `user_prompt_submit`, `pre_tool_use`, `post_tool_use`, `permission_request`, `waiting_for_input`, `stop`, `session_end`, `subagent_start`, `subagent_stop`); hook names map to them in `HookEvent` and `ClaudeNotification`. Subagent events count as progress inside the turn, like tool events. Tool, permission and waiting events that arrive while `idle` or `done` are stale and ignored. `done` marks the session unread only when it is not focused; `Focus()` clears it.
- `NameFor(task, prs)`: naming precedence (pin, PR, Linear title, prompt summary), with `SummarizeText`, `PinName` and `WithTitle`. See [ADR 0026](../../docs/adr/0026-session-naming.md).
- `BannerFor(session, name, effect)` and `Coalescer`: which notify effects become a banner (not for muted sessions) and the one-per-10-s rule per session.
- `PlanCleanup(worktree, facts, now)`: the cleanup decision (`remove`, `backup_then_ask` or `keep` with a reason). See [ADR 0021](../../docs/adr/0021-worktree-cleanup.md).
- `StateSince(session, events)`: when a session entered its state, by replaying its events through `Apply`; `WorktreeLabel(worktrees)`: `repo@branch +N`, as banner titles and the phone show it.
- `Quotas(sessions)`, `Quota.Low`/`Stale`, `Advise(quotas, harness)`: the usage bar and the low-quota warning. See [ADR 0017](../../docs/adr/0017-usage-and-limits-bar.md).
- `OfferFallback(quotas, cfg, request)` and `OfferFallbacks(quotas, cfg, queue)`: a mapped Codex start for a Claude one when Claude's shortest window is under the configured threshold. See [ADR 0027](../../docs/adr/0027-codex-fallback.md).
- `ParseWorkItem`, `PlanSessionStart`, `NextInView`, `AgentTitle`, `DrainLauncher`, `Sidebar`, `BuildSessionCard`, `SubagentTree`: session start, end, title strips, launcher and sidebar rules (see [internal/daemon/](../daemon/AGENTS.md) and [internal/tui/](../tui/AGENTS.md)).
- `Recipe.Validate`, `PlanDeps`, `InstallCommand`: setup recipes (see [internal/adapters/setup/](../adapters/setup/AGENTS.md)).
- `OnboardSteps`, `NextOnboardStep`, `DefaultOnboardPicks`, `HarnessOffer`, `NvimOfferFor`, `OnboardingNeeded`, the `CodexTrustStep` text: the first-run walkthrough (see [ADR 0040](../../docs/adr/0040-first-run-walkthrough.md)).
- `SendableText`, `NextSend`, `Unsend`, `DropSends`, `RequeueSend`: the `session.send` queue (see [internal/daemon/](../daemon/AGENTS.md)).
- Ports rules: `PortsByWorktree`, `KillGroups`. Disk rules: `Reclaimable`, `TotalSize`.
- `Message`, `MessageLog`, `TranscriptLines`, `TranscriptFromHook`, `ToolSummary`, `ToolResult`, `OneLine`, `ClipText`, and for paging and watching `ToolCalls` (finishes a bare tool result from a call seen earlier, drops one whose call it never saw), `NewestPage` (the newest messages without splitting a line) and `TranscriptPageLimit` (50 by default, at most 500): the harness-neutral chat model the Claude and Codex transcript parsers produce (see [internal/adapters/](../adapters/AGENTS.md) and [ADR 0046](../../docs/adr/0046-remote-app.md)).
- Pairing ([ADR 0046](../../docs/adr/0046-remote-app.md)): `NewPairCode` (8 characters from `PairCodeAlphabet`, which drops 0, 1, O, I and L, drawn from an injected `intn`), `NormalizePairCode`, `Pairing.Issue`/`Redeem` (5-minute codes, single use; a wrong code voids nothing; 5 failed tries a minute per address and 20 in all, after which every try is refused and not counted), `NewDevice` (32 random bytes as base64url, stored as `HashDeviceToken`), `CheckDeviceToken` (constant time), `RevokeDevice`, `Device.Seen` (at most once a minute), `DeviceName`, `NewDeviceID`, `PairURL`.
- `MergeLoginPath(current, login)`: the daemon's PATH plus the login shell's entries it lacks, current entries first (see [internal/daemon/](../daemon/AGENTS.md)).
- `NewProject(workspace, name, setup)` and `ProjectOf(projects, path)`: registered projects (see [internal/daemon/](../daemon/AGENTS.md#projects)).

### Discovery

See [ADR 0007](../../docs/adr/0007-workspace-discovery.md). `KindOfRoot`, `ReposIn`, `SingleRepo`, `MergeRepoState`, `LastUsedWorkspace`, and for the dialog's typed path `ParsePathInput` and `CompleteDirs` ([ADR 0047](../../docs/adr/0047-workspace-path-input.md)).

- **Kind.** `<path>/.git` a directory means `single`, with the path itself as the only repo. Anything else is an `orchestration` root: its direct children are scanned, symlinks followed, and each child with a `.git` directory is a repo. A child whose `.git` is a file is a worktree and is skipped, as is a dangling link. Nothing deeper than one level is read. Repos are sorted by name, and a symlinked repo keeps the link's name and path.
- **Budget.** Discovery over 15 repos must finish in < 300 ms; `BenchmarkDiscovery` fails above that.

### Worktrees

`IsWorktreeAdd`, `SubagentParent`, `AttributeWorktree`, `ReclaimWorktrees`, `ReconcileWorktrees`, `RollupChecks`, `PRForBranch`. See [ADR 0012](../../docs/adr/0012-worktree-detection.md).

- **Attribution** (`AttributeWorktree`), for worktrees not seen before: the parent session of a subagent worktree (`<cwd>/.claude/worktrees/agent-*`), then a session whose cwd is inside it, then a `git worktree add` claim from a `PostToolUse` hook in the last 30 s. With claims from several sessions, only one whose command names the path or branch wins. Otherwise unassigned. A scan can run while `git worktree add` does, before the `PostToolUse` claim lands; `ReclaimWorktrees` then attaches an unassigned worktree once a recent claim from one session names its path or branch.

### Review

`RangeFor` (scopes), `TurnRef`/`LatestTurn`/`OlderTurns`/`TurnsOfWorktree`, `ParseDiff`, `IsViewed`, `SplitRows`, and for comments `CommentOn`, `ReviewPrompt` (golden-tested), `ReviewDraft.Queue`/`Dispatch`, `HunkPatch`. The prompt lists `worktree:path:lines`, the quoted code and the comment. See [ADR 0023](../../docs/adr/0023-review-pane.md) and [ADR 0028](../../docs/adr/0028-review-comments-and-hunks.md).
