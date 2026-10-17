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
- `NameFor(task, prs)`: naming precedence (pin, PR title, prompt summary), with `SummarizeText`, `PinName` and `WithTitle`. See [ADR 0026](../../docs/adr/0026-session-naming.md).
- `BannerFor(session, name, effect)` and `Coalescer`: which notify effects become a banner (not for muted sessions), and the rule of one banner every 10 s for each session.
- `PlanCleanup(worktree, facts, now)`: the cleanup decision (`remove`, `backup_then_ask` or `keep` with a reason). See [ADR 0021](../../docs/adr/0021-worktree-cleanup.md).
- `StateSince(session, events)`: when a session went into its state. It finds this when it applies the events of the session through `Apply` again.
- `WorktreeLabel(worktrees)`: `repo@branch +N`, as banner titles and the phone show it.
- `Quotas(sessions)`, `Quota.Low`/`Stale`, `Advise(quotas, harness)`: the usage bar and the low-quota warning. See [ADR 0017](../../docs/adr/0017-usage-and-limits-bar.md).
- `OfferFallback(quotas, cfg, request)`: a mapped Codex start instead of a Claude start, when the shortest window of Claude is under the configured threshold. See [ADR 0027](../../docs/adr/0027-codex-fallback.md).
- `ParseWorkItem`, `PlanSessionStart`, `NextInView`, `AgentTitle`, `Sidebar`, `BuildSessionCard`, `SubagentTree`: session start, end, title strips and sidebar rules (see [internal/daemon/](../daemon/AGENTS.md) and [internal/tui/](../tui/AGENTS.md)).
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
- `ParseBrowserCommand(args)` and `BrowserCommands()`: the commands and arguments of `crew browser` (see [ADR 0051](../../docs/adr/0051-browser-in-mac-app.md)).
- Automations ([ADR 0050](../../docs/adr/0050-automations.md)): `ValidateAutomation`, `MergeAutomations` (a repo automation replaces the global one with the same name) and `ExpandPrompt` (`{{context}}`, `{{work_item}}`, `{{repo}}`, `{{branch}}`, `{{event}}`; context without `{{context}}` goes at the end).
- Mates ([ADR 0053](../../docs/adr/0053-mates.md)):
  - `ValidateMate` (a one-word lower case name of 32 characters or less, a prompt, the agent `claude` or `codex`, a known effort, an owner that is a Slack user ID or empty, and repos that are a name or a path in a registered workspace, with no repeat), `CheckMates` (the valid mates, and one error for each invalid or repeated mate, so the other mates still load), and `MateTOML`, `SetMateBlock` and `RemoveMateBlock` (the `[[mate]]` tables of `config.toml`). The definition has no token.
  - `MateManifest(mate)` makes the Slack app manifest as JSON: the bot user, the scopes (`app_mentions:read`, `chat:write`, `channels:history`, `groups:history`, `im:history`, `im:read`, `im:write`, `users:read`), the events (`app_mention`, `message.channels`, `message.groups`, `message.im`), interactivity and `socket_mode_enabled`. `testdata/mate_manifest.golden` pins it. `MateManifestURL` makes the api.slack.com link. `ValidateMateTokens` checks the `xoxb-` and `xapp-` prefixes, and its errors never hold a token. `ValidMateName` is the name rule alone.
  - `NewMateClaim(mate, code, now)` and `MateClaim.Accepts(user, text, now)`: the one-time owner code is good for 10 minutes (`MateClaimTTL`). It accepts only a Slack user ID and a text that is the code (`NormalizePairCode`). `MateConnection` is the state of the Slack link of a mate.
  - `ClassifyAction(harness, tool, input, scope)` gives an `Action` with a kind: `safe`, or a dangerous kind (`merge`, `create_pr`, `push`, `delete_branch`, `outside_worktree`, `deploy`, `external_write`, `secret`, `unknown`). `Input` is the compact JSON of the tool input.
  - Shell commands go through `ClassifyShell`. It splits chains (`&&`, `||`, `;`, `|`, `&`, new lines, parentheses), removes quotes, follows wrappers (`env`, `command`, `exec`, `nohup`, `time`, `nice`, `timeout`, `xargs`, `eval`, `sh -c`) and `git -C`, and tracks `cd`. It checks each write path and redirection against `scope.Root`. `scope.Real` resolves symlinks.
  - A command, a tool or an input that it does not know or cannot parse is `unknown`, thus dangerous. Command substitution is always `unknown`.
  - `Gate(authority, owner, action, approvals, now)` (the daemon gives it `ChainAuthority(seat, kind)`): a safe action runs. A dangerous action runs only when the authority is the owner, or when a granted `Approval` of the same action is less than 24 h old (`Use` is its index, for one use). With no owner, nobody can run a dangerous action.
  - `GateDenyOutput(action)` is the `PreToolUse` deny that the hook prints.
  - `DecideApproval(approval, by, owner, allow, now)`: only the owner decides (`ErrApprovalNotOwner`), only a pending approval (`ErrApprovalDecided`), and only for 24 h (`ErrApprovalExpired`). `OpenApprovals` gives the pending approvals that did not expire, oldest first. `ApprovalRequestText`, `ApprovalOutcomeText`, `ApprovalSessionPrompt` and `ApprovalPush` are the texts for Slack, the session and the push.
  - `MateThread` and `MateThreadKey`: the link of a Slack thread to a session. `MateFirstPrompt(mate, asker, thread)` (`testdata/mate_first_prompt.golden` pins it) and `MateReplyPrompt(asker, text)` put the Slack text in `slack-thread` tags as data, and change each tag in the text so that the text cannot close the tags. `MateWorkspace(mate, workspaces)` gives only a single-repo workspace of a repo in `repos`. `NextMateAuthority(current, user, owner, busy)` and `MateResultText(text)` (a maximum of `MateResultMax` characters, "Done." when empty).
  - Mate to mate (`mate_guard.go`). `MateSeat` also has `Origin` (the Slack user ID of the person who started the chain), `Asked` (the dangerous kinds that this person asked for) and `Hops` (the number of mate-to-mate steps).
    - `DirectMateTurn(seat, user, text)`: a person posted. The authority and the origin are that person, `Asked` is `AskedActions(text)` (the words merge, PR or pull request, push, delete … branch, deploy) and `Hops` is 0.
    - `ChainMateTurn(seat, from, author)`: a mate posted. The authority is the bot user of that mate, the origin and `Asked` come from its seat, and `Hops` is one more.
    - `ChainAuthority(seat, kind)` is the owner only when the origin is the owner of this mate and `kind` is in `Asked`. Else it is the authority. Thus a chain that a different person started never gets the owner, and an owner chain gets only the asked action. `NextMateSeat(current, next, busy)` uses the `NextMateAuthority` rule on the full seat. `MateMentions(text, user)` finds `<@user>`.
    - `MateLimits` (`DefaultMateLimits`: 5 hops, 20 turns each hour, 100 turns each day, no token limit, no stop; 0 turns a limit off) and `MateUsage` (`AddTurn` keeps 24 h of turns, `AddTokens` counts the tokens of one local day).
    - `MateGuard(limits, hops, usage, now)` gives the first stop, in this order: `stopped`, `hop_limit` (hops more than the limit), `daily_tokens`, `daily_turns`, `rate_limit`. Else it gives `GuardNone`. `MateGuardText` tells why.
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

`RangeFor` (scopes), `TurnRef`/`LatestTurn`/`OlderTurns`/`TurnsOfWorktree`, `ParseDiff`, `IsViewed`, `SplitRows`, and for comments `CommentOn`, `ReviewPrompt` (golden-tested), `ReviewDraft.Queue`/`Dispatch`, `HunkPatch`. The prompt lists `worktree:path:lines`, the quoted code and the comment. See [ADR 0023](../../docs/adr/0023-review-pane.md) and [ADR 0028](../../docs/adr/0028-review-comments-and-hunks.md).
