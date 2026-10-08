# internal/domain

Pure types and rules. This package does no IO and does not import other `internal/*` packages. `Workspace`, `Repo`, `Task`, `Session`, `Worktree`, `Harness`, `AgentState`, `Usage`, `ReviewDraft`, `Comment`, `CleanupPlan`.

## Tests

- All code here has table tests, with no mocks. Tests are in the package (`package domain`). depguard bans imports of `internal/...` from `domain`, and this also applies to an external `domain_test` package that imports `domain`.
- CI runs `gremlins` here. The mutation score must stay ≥ 80%.
- `testdata/review_prompt.golden` pins the review prompt format. Change it only on purpose, and update the golden by hand.

## Rules

- `Session.Apply(event) (Session, []Effect)`: the state machine.
  - Adapters map hooks to harness-neutral events: `session_start`, `user_prompt_submit`, `pre_tool_use`, `post_tool_use`, `permission_request`, `waiting_for_input`, `stop`, `session_end`, `subagent_start`, `subagent_stop`. `HookEvent` and `ClaudeNotification` map hook names to them.
  - Subagent events count as progress in the turn, as tool events do.
  - Tool, permission and waiting events that arrive while `idle` or `done` are stale. `Apply` ignores them.
  - `done` marks the session unread only when it does not have focus. `Focus()` clears the mark.
- `NameFor(task, prs)`: naming precedence (pin, PR, Linear title, prompt summary), with `SummarizeText`, `PinName` and `WithTitle`. See [ADR 0026](../../docs/adr/0026-session-naming.md).
- `BannerFor(session, name, effect)` and `Coalescer`: which notify effects become a banner (not for muted sessions), and the rule of one banner every 10 s for each session.
- `PlanCleanup(worktree, facts, now)`: the cleanup decision (`remove`, `backup_then_ask` or `keep` with a reason). See [ADR 0021](../../docs/adr/0021-worktree-cleanup.md).
- `StateSince(session, events)`: when a session went into its state. It finds this when it applies the events of the session through `Apply` again.
- `WorktreeLabel(worktrees)`: `repo@branch +N`, as banner titles and the phone show it.
- `Quotas(sessions)`, `Quota.Low`/`Stale`, `Advise(quotas, harness)`: the usage bar and the low-quota warning. See [ADR 0017](../../docs/adr/0017-usage-and-limits-bar.md).
- `OfferFallback(quotas, cfg, request)` and `OfferFallbacks(quotas, cfg, queue)`: a mapped Codex start instead of a Claude start, when the shortest window of Claude is under the configured threshold. See [ADR 0027](../../docs/adr/0027-codex-fallback.md).
- `ParseWorkItem`, `PlanSessionStart`, `NextInView`, `AgentTitle`, `DrainLauncher`, `Sidebar`, `BuildSessionCard`, `SubagentTree`: session start, end, title strips, launcher and sidebar rules (see [internal/daemon/](../daemon/AGENTS.md) and [internal/tui/](../tui/AGENTS.md)).
- `Recipe.Validate`, `PlanDeps`, `InstallCommand`: setup recipes (see [internal/adapters/setup/](../adapters/setup/AGENTS.md)).
- `OnboardSteps`, `NextOnboardStep`, `DefaultOnboardPicks`, `HarnessOffer`, `NvimOfferFor`, `OnboardingNeeded`, the `CodexTrustStep` text: the first-run walkthrough (see [ADR 0040](../../docs/adr/0040-first-run-walkthrough.md)).
- `SendableText`, `NextSend`, `Unsend`, `DropSends`, `RequeueSend`: the `session.send` queue (see [internal/daemon/](../daemon/AGENTS.md)).
- Ports rules: `PortsByWorktree`, `KillGroups`. Disk rules: `Reclaimable`, `TotalSize`.
- The harness-neutral chat model that the Claude and Codex transcript parsers produce:
  - `Message`, `MessageLog`, `TranscriptLines`, `TranscriptFromHook`, `ToolSummary`, `ToolResult`, `OneLine`, `ClipText`.
  - For paging and watching:
    - `ToolCalls` completes a bare tool result from a call that it saw before. It drops a result whose call it never saw.
    - `NewestPage` gives the newest messages and does not split a line.
    - `TranscriptPageLimit` is 50 by default, a maximum of 500.

  See [internal/adapters/](../adapters/AGENTS.md) and [ADR 0046](../../docs/adr/0046-remote-app.md).
- Pairing ([ADR 0046](../../docs/adr/0046-remote-app.md)):
  - `NewPairCode`: 8 characters from `PairCodeAlphabet`, drawn from an injected `intn`. The alphabet does not have 0, 1, O, I and L.
  - `NormalizePairCode`.
  - `Pairing.Issue`/`Redeem`: codes are good for 5 minutes and for one use. A wrong code does not void a code. The limit is 5 failed tries each minute for each address, and 20 in total. After the limit, each try is refused and not counted.
  - `NewDevice`: 32 random bytes as base64url, stored as `HashDeviceToken`.
  - `CheckDeviceToken` (constant time), `RevokeDevice`, `Device.Seen` (a maximum of one time each minute), `DeviceName`, `NewDeviceID`, `PairURL`.
- `MergeLoginPath(current, login)`: the PATH of the daemon, and the entries of the login shell that it does not have. Current entries come first (see [internal/daemon/](../daemon/AGENTS.md)).
- Registered projects: `NewProject(workspace, name, setup)`, `ProjectOf(projects, path)`, `ProjectOfWorktree` (by main checkout, then by path) and `ProjectRows` (the sidebar rows, by name, with the worktrees of each). See [internal/daemon/](../daemon/AGENTS.md#projects).
- Tabs of a project worktree:
  - `TabHome`: the project worktree of the strip of a session. This is its `Tab`, else the first project worktree that it holds. Sessions outside projects have none.
  - `WorktreeTabs`: the live agents that hold the worktree or are tabs in it, and its `ShellTab`s, in the order they opened.
  - `StepTab` (with wrap).
  - `TabAfterClose`: an adjacent tab, right first, only when the shown tab closes.
  - `TabStrip` and `TabbedTitle`: the strip in front of the own title of the pane.
  - `Session.WorksIn`: the worktree of a tab, else the worktrees that the session owns. Banner labels and review targets use it.

### Discovery

See [ADR 0007](../../docs/adr/0007-workspace-discovery.md). `KindOfRoot`, `ReposIn`, `SingleRepo`, `MergeRepoState`, `LastUsedWorkspace`. For the typed path of the dialog: `ParsePathInput` and `CompleteDirs` ([ADR 0047](../../docs/adr/0047-workspace-path-input.md)).

- **Kind.** If `<path>/.git` is a directory, the kind is `single`, and the path itself is the only repo. All other paths are an `orchestration` root:
  - Discovery scans its direct children and follows symlinks. Each child with a `.git` directory is a repo.
  - A child whose `.git` is a file is a worktree. Discovery skips it, and it also skips a dangling link.
  - Discovery reads only one level.
  - Repos are sorted by name. A symlinked repo keeps the name and path of the link.
- **Budget.** Discovery over 15 repos must finish in < 300 ms. `BenchmarkDiscovery` fails above that.

### Worktrees

`IsWorktreeAdd`, `SubagentParent`, `AttributeWorktree`, `ReclaimWorktrees`, `ReconcileWorktrees`, `RollupChecks`, `PRForBranch`. See [ADR 0012](../../docs/adr/0012-worktree-detection.md).

- **Attribution** (`AttributeWorktree`) is for worktrees not seen before. It uses this order:
  1. The parent session of a subagent worktree (`<cwd>/.claude/worktrees/agent-*`).
  2. A session whose cwd is in the worktree.
  3. A `git worktree add` claim from a `PostToolUse` hook in the last 30 s. If several sessions claim it, only a session whose command names the path or branch wins.

  Else the worktree is unassigned. A scan can run while `git worktree add` runs, before the `PostToolUse` claim arrives. Then `ReclaimWorktrees` attaches an unassigned worktree when a recent claim from one session names its path or branch.
- **Tab sessions** (`Session.Tab` set: an extra agent opened in a project worktree) never own a worktree. Attribution and reclaim drop hints and claims marked `Tab`. Thus a worktree and its PR stay with the session that made it, and a tab names no PR.

### Review

`RangeFor` (scopes), `TurnRef`/`LatestTurn`/`OlderTurns`/`TurnsOfWorktree`, `ParseDiff`, `IsViewed`, `SplitRows`, and for comments `CommentOn`, `ReviewPrompt` (golden-tested), `ReviewDraft.Queue`/`Dispatch`, `HunkPatch`. The prompt lists `worktree:path:lines`, the quoted code and the comment for each comment. See [ADR 0023](../../docs/adr/0023-review-pane.md) and [ADR 0028](../../docs/adr/0028-review-comments-and-hunks.md).
