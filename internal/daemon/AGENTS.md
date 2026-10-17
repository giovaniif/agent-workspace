# internal/daemon

Socket server, event loop and workers. This package connects the adapters to `app`. Do not exec in this package: tests here use fakes, and `test/integration` runs it with real adapters. Do not import `internal/adapters/tmux` from any other package. The wire protocol is in [internal/rpc/](../rpc/AGENTS.md).

## Process

`crew daemon` runs in the foreground until SIGINT/SIGTERM. `crew daemon start` starts it detached (`setsid`, output to `$CREW_HOME/daemon.log`) and waits for the socket. `daemon status` prints pid, uptime, and session and worktree counts. If no daemon runs, it exits 1 with `not running`, and it never starts one. `daemon stop` sends SIGTERM and waits until the pid file is removed.

On start, before all other work, the daemon adds the PATH entries of the login shell that its own PATH does not have (`loginshell.Path` with a 1 s cap, `domain.MergeLoginPath`). If this fails, the daemon logs it and does not change PATH. tmux gives each new pane the PATH of the daemon. The process that started the daemon (a bare `ssh`, launchd, systemd) can be without `~/.local/bin` and similar entries (#153). The e2e `login_path.txtar` covers this.

Files in `$CREW_HOME` (default `~/.crew`):

| File | Purpose |
|---|---|
| `crew.lock` | `flock` held for the daemon's life; the kernel drops it if the daemon dies. A second daemon exits 1 with `already running (pid N)`. |
| `crew.pid` | written after taking the lock, removed on clean exit |
| `crew.sock` | the socket, mode 0600; a stale one is removed by the next lock holder |
| `state.db` | the SQLite store; loaded once on start |

**Event loop.** One goroutine owns all state.

- Adapters call `Daemon.Post(event)` with `WorkspaceChanged`, `TaskChanged`, `WorktreeChanged` or `SessionChanged`. Each event replaces the entity by key (`WorkspaceRemoved` deletes it), queues a store write, increments `seq` and sends the diff to all subscribers in order.
- Connections read state only through closures that run on the loop.
- Each connection has an outbox of 1024 messages. If a connection falls behind, the daemon disconnects it and never waits for it.
- The loop parses hooks (a small JSON decode). git, gh, tmux and `du` never run on it.

## Workspaces

See [ADR 0007](../../docs/adr/0007-workspace-discovery.md). The discovery rules are in [internal/domain/](../domain/AGENTS.md).

- `workspace.add` answers from the filesystem only and publishes the workspace. Then it refreshes repo facts off the loop, and publishes again only if something changed. The daemon does this again for all workspaces every 30 s.
- Each `add` sets `Workspace.LastUsed`, which is stored with the workspace. `workspace.list` returns the most recent one as `last_used`. The new-session dialog uses it as the default.

## Projects

`go test ./internal/domain/ ./internal/daemon/ ./internal/adapters/sqlite/ -run Project`.

A project (`domain.Project`: `Root`, `Name`, `Setup`) is a registered workspace root (single repo or orchestration), and an optional machine-local setup script. The script is in the `projects` table, never in the repo.

- `project.add` discovers the path off the loop. It refuses a path with no repos (`domain.NewProject`). It adds the workspace as `workspace.add` does, then publishes the project. If you add the same root again, it replaces the name and script.
- `project.remove` forgets the project and keeps the workspace.
- To open a project is `session.new` with its root as `workspace`, so it works from any cwd.
- `domain.ProjectOf` finds the project that holds a path. The deepest root wins.
- **Setup script.** When `session.new` creates a worktree in the workspace of a project, the `Setup` of the project runs there as `sh -c <script>` (`WithProjectSetup`, `setup.Shell`). It runs after the `.crew.toml` recipe and before the harness starts. An empty script runs nothing.
  - A failing script fails the start, as a failing recipe does, and keeps the worktree.
  - An orchestration root starts with no worktree, so nothing runs there. Worktrees that an agent adds itself do not get the setup.
  - Tests: `go test ./internal/app/ ./internal/daemon/ -run 'ProjectSetup|SetupScript'`.
- **Cleanup.** A project holds a worktree whose main checkout (or, if not available, whose path) is in the root of the project.
  - `PlanCleanup` keeps such a worktree with the reason `held by project <name>`, before all other rules. Thus the scheduled cleanup and the remove action of the disk view do not touch it. When its session ends, it stays.
  - To let cleanup decide as before, remove the project.
  - Tests: `go test ./internal/domain/ ./internal/app/ ./internal/daemon/ -run Cleanup`.

## Tabs

`go test ./internal/domain/ ./internal/daemon/ -run Tab`.

A worktree that a project holds is a container of tabs: the agent that made it, extra agents opened in it, and shells. The rules are in [internal/domain/](../domain/AGENTS.md). Sessions outside projects have no tabs. Each `tab.*` call on such a session is `bad_request`.

- **Methods** (`rpc.TabParams`, each → the `domain.Tab` it acted on):
  - `tab.new` (`{"session","kind":"shell"|"agent","harness","model","effort"}`) opens a tab in the worktree of the strip of `session` (`domain.TabHome`). It puts the tab in the main slot with focus.
  - An agent tab is a session with `Tab` set to the worktree, the task of the host, `Dir` set to the worktree path, and no `WorktreeIDs`.
  - A shell tab is a `domain.ShellTab`, in memory only (`State.ShellTabs`, `Diff.ShellTab`, `Diff.RemovedShellTab`). Thus a daemon restart forgets it. When its worktree is removed, the scanner drops it and kills its pane.
  - `tab.show` (`{"session","tab"}`) and `tab.step` (`{"session","delta"}`, with wrap) swap a tab into the slot. They do not move keyboard focus, so the sidebar keeps it.
  - `tab.close` (`{"session","tab"}`; with no `tab`, it closes the shown tab) kills only that pane and shows an adjacent tab (`domain.TabAfterClose`).
  - To close an agent tab ends that session. To close the last tab is `session.end`. Thus the worktree stays with its owner and the project, and `u` resumes it.
- **Active tab.** The shown tab is the tab whose pane is in the slot. The daemon also keeps the last active tab for each worktree (`State.ActiveTabs`, `Diff.ActiveTab`). `tab.*` and `session.focus` set it.
- **Strip.** The 250 ms title worker puts `domain.TabStrip` in front of the own title of each tab pane (`domain.TabbedTitle`), and marks the tab of that pane. Thus the strip above the slot always shows the tab in the slot. tmux 3.4 reports no mouse column on a pane border, so you cannot click the strip. You can click the tab line of the sidebar (see [internal/tui/](../tui/AGENTS.md)).
- **Attention and review.** An agent tab is a session. Thus its state, unread mark, banners, pushes and `space` work as for all sessions.
  - Its banner title names the worktree that it is a tab of (`Session.WorksIn`).
  - Its name never has the PR of the worktree.
  - Its review and turn snapshots cover that worktree under its own session ID, so turns stay with each agent.
- **Attribution.** A tab session never owns a worktree. The daemon passes its hook cwd and `git worktree add` claims as `Tab` hints and claims, and attribution drops them. The worktree and its PR stay with the session that made it.

## Sessions

See [ADR 0015](../../docs/adr/0015-session-lifecycle.md).

- **Start.** `n` in the TUI or `crew new` calls `session.new`.
  - The work item is a GitHub PR URL or text. A URL of a different shape stays text (`domain.ParseWorkItem`). It can be empty. Then the session gets its own text task, and its first `UserPromptSubmit` prompt becomes the text of that task (`domain.NameFromPrompt`).
  - A single repo gets one worktree at `$CREW_HOME/worktrees/<repo>/<slug>`, branched from `origin/<default>`, and its setup recipe runs.
  - An orchestration root starts at the root with no worktree (`domain.PlanSessionStart`).
- **First prompt.** `session.new` takes an optional `prompt`.
  - A fresh session is `idle` when its pane exists, but the harness is still booting. Thus the daemon queues the text in `State.Sends` and holds it (`state.booting`).
  - The hold stays until the first `SessionStart` or `UserPromptSubmit` hook of the harness. Both Claude and Codex install `SessionStart`. Then the text goes out through the normal `session.send` path.
  - A hook can arrive after the pane starts but before `session.new` commits the session. The daemon keeps a hook for an unknown pane for 30 s (`state.earlyHooks`, a maximum of 16 for each pane and of 64 panes). When it must keep a hook for a 65th pane, it first removes the expired panes, and then the pane with the oldest hook. When `session.new` commits the session on that pane, the daemon applies the kept hooks again.
  - If no such hook arrives in 15 s (`WithFirstPromptGrace`), the daemon removes the hold. This occurs when hooks are missing.
  - If the session ends first, the daemon drops the text.
  - Tests: `go test ./internal/daemon/ -run NewSessionPrompt`.
- **Budget.** `session.new` and `session.focus`, without the setup recipe, must finish in < 1 s. The `NewSession` integration test checks it (`go test -tags integration -run NewSession ./internal/daemon/`, with real temp repos, a fake harness script and its own tmux socket).
- **Focus.** `enter`, and each new session, calls `session.focus`. It swaps the pane into the main slot, sets `Session.Focused` and clears unread. Daemon start clears focus.
- **End.** `x` then `y` calls `session.end`. The daemon kills the pane, and the session goes `idle` with no pane and `Ended`. This removes it from the sidebar.
  - It stays in the state, hidden, until its last worktree is removed. Then the daemon deletes it and its events (`Session.Forgotten`). A session with no worktree is deleted immediately. See [ADR 0035](../../docs/adr/0035-ended-sessions.md).
  - If it was the session in view, the daemon shows the next session in sidebar order (`domain.NextInView`, selected before the end is published). If no session is left, it shows an empty-state pane.
  - The daemon does the same when an agent exits by itself: a 2 s worker sees that the slot lost its pane and ends the session in view.
- **Resume.**
  - The daemon keeps the `session_id` of each hook (Codex notify: `thread-id`) as `Session.ResumeID`. The newest wins (the Claude `/clear` starts a new one).
  - `Session.Dir` is the dir that the agent started in.
  - `session.resume` (`{"id"}`) starts an ended session again in that dir with `claude --resume <id>`, `codex resume <id>` or `omp --resume <id>`, with the same model and effort. It keeps its ID, task and worktrees.
  - A live session, or a session without one of the two fields, is a bad request. See [ADR 0042](../../docs/adr/0042-resume-ended-sessions.md).
- **Transcript.** The daemon keeps the `transcript_path` of each hook as `Session.Transcript`. The newest wins: resume and the Claude `/clear` start a new file. It is stored with the session, so it is kept after a restart. The remote app reads it (see [ADR 0046](../../docs/adr/0046-remote-app.md)). Nothing reads it on the loop.
- **Title strips.** Each pane has a top border. The border of an agent pane shows `domain.AgentTitle`: state, harness, model, effort, cwd, then a tab for each worktree with its PR.
  - A 250 ms worker sets it through `TerminalHost.SetTitle`.
  - The worker copies state on the loop only when it changed. It formats and runs tmux off the loop, only for changed titles. See [ADR 0038](../../docs/adr/0038-pane-title-strips.md).
- **Survival.** The daemon and the tmux server own sessions. Thus, when you quit the TUI or detach, nothing changes. On daemon start, `app.ReconcilePanes` ends restored sessions whose pane is gone, off the loop.
- **Model and effort.** `M`/`E` in the TUI send `session.switch`.
  - For Claude, the daemon types `/model <x>` or `/effort <y>` into the pane when the agent is between tools.
  - For Codex, it types `/model` and moves through the picker with arrow keys ([ADR 0034](../../docs/adr/0034-codex-model-picker-switching.md), integration test in [test/integration/](../../test/integration/AGENTS.md)).
  - For omp, it types `/switch <id>` or `/switch <model>:<level>`.
  - Tests use a fake terminal host. To test by hand, run the TUI against a fake `claude` on `PATH`. See [ADR 0018](../../docs/adr/0018-model-effort-switching.md).
- **Send and interrupt.** `go test ./internal/domain/ ./internal/daemon/ -run 'SessionSend|SessionInterrupt'`, and `-tags integration -run SessionSend` for a real tmux pane. See [ADR 0046](../../docs/adr/0046-remote-app.md).
  - `session.send` (`{"session","text"}` → `{"id","queued"}`) adds to its own text queue: `State.Sends` / `Diff.Sends`. This is the full list of `domain.QueuedSend` `{"id","session","text","queued_at"}`, oldest first, in memory only.
  - `domain.NextSend` selects the oldest send of the session when the session is idle, done or waiting, and nothing is in flight. The send goes out as one bracketed paste and Enter. It stays in flight until the next hook of the session. Thus each free turn takes one send.
  - A review draft and a send never share a turn. Each one waits while the other pastes or is in flight.
  - A failed paste goes back to the front of the queue of its session.
  - `session.unsend` (`{"session","id"}`) drops a queued send. After the send went out, it gives `not_found`.
  - When a session ends, the daemon drops its queue.
  - `session.interrupt` (`{"session"}`) sends Escape to the pane on the connection goroutine.
- **Harnesses.** The daemon applies the model of a Codex hook immediately. A worker reads the rollout, never the loop. An omp hook has `model` and `effort`, and the loop reports them, with no session-file read (see [internal/adapters/](../adapters/AGENTS.md)).

## Naming

`go test ./... -run Naming` runs all naming tests: domain rules, resolvers, the `gh` adapter (against a fake `gh`), the daemon and the TUI.

- The name comes from the work item. It is resolved off the loop: after `session.new`, a worker asks `app.TitleResolvers` and merges the title into the task. The resolver is `gh pr view`.
- Tests never read the token.
- In the TUI, `R` calls `session.rename` (`{"id","name"}`) to rename and pin. `A` calls `session.unpin` (`{"id"}`). See [ADR 0026](../../docs/adr/0026-session-naming.md).

## Config

`go test ./internal/domain/ ./internal/daemon/ -run 'Config|TOML'`. `WithConfig(path)` turns on `config.get` and `config.set` (protocol in [internal/rpc/](../rpc/AGENTS.md)). `Run` passes `$CREW_HOME/config.toml`. They run on the connection goroutine under one mutex, never on the loop.

- **Editing.** `domain.SetTOMLValue` edits the text line by line. It replaces `key = …` in `[table]`, adds the key at the end of that table, or adds the table at the end.
  - Thus the comments, order and unknown keys of the user stay. A comment on the replaced line itself goes.
  - An empty value removes the line.
  - Before it writes, it parses the result again. The result must hold exactly the new value.
  - It refuses a key that is set in a different way (an inline table, dotted keys), with a message to edit it by hand. It never touches a file that does not parse.
- **Backup.** A changed file is first copied to `config.toml.crew-<UTC time>.bak` (mode 600). Then it is replaced atomically with its old mode. If you set the value that it already has, nothing is written and there is no backup. A missing file is created without a backup.
- **Live.** After a write, the daemon reloads `[defaults.<harness>]` and `[push] away_after`. `[push] away_after` reloads only when presence runs. To turn it on from `"0"`, restart. The TUI reads `[fallback]` and `[theme]` when it starts.

## Automations

`go test ./internal/domain/ ./internal/daemon/ -run 'Automation|Schedule|NextRun'`. See [ADR 0050](../../docs/adr/0050-automations.md) and the methods in [internal/rpc/](../rpc/AGENTS.md).

- **Schedule.** `WithAutomationSchedule(max)` starts one timer goroutine. It never polls on the loop. Each wake reads the definitions off the loop, then sleeps until the next due run, a maximum of `max` (`Run` passes 1 min, so config changes apply in a minute).
  - `schedule` is `hourly`, `daily HH:MM`, `weekdays HH:MM`, `weekly <day> HH:MM` or a 5-field cron expression (`domain.ParseSchedule`). `timezone` is an IANA name. The default is the local time of the daemon.
  - The next run comes after the newest scheduled run of the automation. If there is none, it comes after the time that the daemon first saw the automation.
  - `domain.ScheduleDecision`: a due run that is late by 15 min or less runs once. An older one is recorded as `missed`, once, for the newest missed time.
  - If the session of the last run is still live, the run is recorded as `skipped` (except with `session = "reuse"`).
  - In a DST gap, a run starts at the first time after the gap. In a repeated hour, it starts once.
- **Events.** `on = [...]` starts a run when the loop sees a change (`domain.WorktreeAutomationEvents`, `domain.SessionAutomationEvents`): `pr.opened`, `pr.ci_failed`, `pr.review_requested`, `pr.merged`, `worktree.created`, `session.finished` and `session.needs_attention`.
  - The loop only puts the event in a bounded queue. A worker matches it (`domain.MatchAutomationEvent`) and starts the run off the loop. A full queue drops the event and logs it.
  - `[automation.filter]` takes `repo` (a name or a path), `branch` (a glob) and `author` (the GitHub login of the PR). A repo automation sees only the events of its repo.
  - The PR link (or the session task) is `{{work_item}}`. A text that describes the event is `{{event}}`.
  - There is one run for each automation and subject (the PR link, the session or the worktree) at a time. While that run is live, a new event records a `skipped` run.

## Mates

`go test ./internal/domain/ ./internal/daemon/ -run Mate`. See [ADR 0053](../../docs/adr/0053-mates.md) and the methods in [internal/rpc/](../rpc/AGENTS.md).

- `mate.*` use the `config.toml` path, the mutex and the backup of the Config section. They run on the connection goroutine, never on the loop.
- A load validates each `[[mate]]` table against the registered workspaces (`domain.CheckMates`). An invalid or repeated mate gives a problem with the file path, and the other mates load.
- A write parses the new text again. The result must hold exactly the mate that was set. If it does not, the daemon refuses and tells you to edit the file by hand. It never touches a file that does not parse.
- Tokens are not part of a mate. `WithMateSecrets(app.MateSecrets)` turns on `mate.set_tokens` and `mate.clear_tokens`. `Run` passes `secrets.Files` for `$CREW_HOME/secrets`. The mate must exist. The daemon validates the prefixes (`domain.ValidateMateTokens`) before it stores anything, and it never logs or returns a token. `mate.remove` clears the tokens of the mate. Tests: `go test ./internal/daemon/ -run MateSetTokens\|MateClearTokens\|NoMateMethod`.
- **Slack worker.** `WithSlack(app.SlackConnector, every)` starts one goroutine that syncs the mates: at start, after each `mate.set`, `mate.remove`, `mate.set_tokens` and `mate.clear_tokens`, and every `every` (`Run` passes `slack.New` and 30 s). It never runs on the loop.
  - Each enabled mate with both tokens gets its own goroutine that runs `SlackMate.Run`. A mate that is not enabled is `paused`. A mate with no tokens is `no_tokens`. When the tokens change (a SHA-256 of them), the worker stops and starts again. When the mate goes, it stops.
  - The status from the adapter is the state of the mate in `mate.list`. After a token error, the mate stays in `error` until its tokens change. The log has the Slack error, never a token.
  - After each sync, each state change of a mate and each turn that runs, the worker makes the board (`domain.NewMateBoard`) and posts `MatesChanged` to the loop only when the board changed. It takes the board lock of the worker first and lets it go before it posts, because the loop also takes that lock. A thread link is a `MateThreadChanged` event, so subscribers get `mate_thread`.
  - `mate.pause`, `mate.resume` and `mate.stop_all` edit `config.toml` and start a sync. Tests: `go test ./internal/daemon/ -run 'MateStopAll|MatePause|MateThreadsAre|MateSetup'`.
- **Owner claim.** `mate.claim` draws a code with `crypto/rand` (`domain.NewPairCode`) and keeps one `domain.MateClaim` for each mate in memory. A direct message to that mate whose text is the code (case, spaces and `-` do not count), from a Slack user ID, before the 10 minutes end, writes that user ID to `owner` with the backup of `config.set`. Then the mate replies in the direct message. The code works one time. A mention or a message in a channel never claims. Tests: `go test ./internal/daemon/ -run 'MateWorker|MateClaim|MateToken|MateNewTokens|MateClearTokens'`.

## Mate threads

`go test ./internal/domain/ ./internal/daemon/ ./internal/adapters/sqlite/ -run 'Mate'` and `go test -tags integration ./internal/daemon/ -run MateThreadIntegration`. See [ADR 0053](../../docs/adr/0053-mates.md).

- The Slack worker hands each mention and each thread message of a mate to its own goroutine, never to the loop. The same message (channel and timestamp) counts one time, because Slack sends a mention in a thread as two events. One mutex puts the thread events in order.
- **Link.** `domain.MateThread` (team, channel, thread timestamp, mate) points to a session. It is in `state.mateThreads` and in the `mate_threads` table, so a restart keeps it. `Status` is the timestamp of the status message.
- **Start.** A mention in a thread with no live link starts a session through the `session.new` path:
  - `domain.MateWorkspace` selects a registered single-repo workspace whose repo is in `repos`, in the order of `repos`. Thus a repo out of the mate is never used. If there is none, the mate posts the error in the thread.
  - The first prompt is `domain.MateFirstPrompt`: the mate prompt, the thread from `conversations.replies` with display names, and the person who asked. The Slack text is in `slack-thread` tags as data.
  - The daemon sets `Session.Mate` with the owner and the authority (the Slack user ID of the event), records the link and posts "Working on it." as the status message.
- **Reply.** A message in a linked thread goes to the same session through `session.send` (`domain.MateReplyPrompt`), so it waits in the queue while the session is busy. A thread message with no link does nothing.
  - `domain.NextMateAuthority` sets the authority. When the session is busy (in a turn, a send in flight, or a send in the queue), the owner cannot raise the authority of a turn that a different person started. The owner can only keep it or lower it.
- **Post back.** For each `PreToolUse` of a mate session, the status message shows the tool (a maximum of one update each 5 s). At `Stop`, the status changes to "Done." and the mate posts `domain.MateResultText` (the last assistant message) as a new message. At `SessionEnd`, the status changes to "Stopped.". Errors go in the thread.

## Codex fallback

`go test ./... -run Fallback`. `[fallback]` in `$CREW_HOME/config.toml` sets `threshold` and the `models`/`efforts` maps from Claude to Codex. Under the harness row, the new-session dialog shows the low-quota warning of `domain.Advise`. `ctrl+s` takes the offer: it switches to the other harness when that harness has reported limits. See [ADR 0027](../../docs/adr/0027-codex-fallback.md).

## Attention and banners

`go test ./... -run Notify` runs all notification tests. The daemon does the effects that `Session.Apply` returns. The adapters are in [internal/adapters/](../adapters/AGENTS.md).

- `EffectNotify` becomes a `domain.Banner` on the loop (`BannerFor`, then `Coalescer`), with no IO.
- A worker reads a bounded queue and calls `app.Notifier`. For a focused session, it first asks `app.Foreground` if a terminal app is in front. If so, it drops the banner.
- The title is the session name (`NameFor`, else the harness). The body is `needs permission`, `waiting` or `done`. There is a maximum of one banner for each session every 10 s.
- `$CREW_HOME/notify.json` sets an optional macOS sound for each event: `{"sounds":{"permission":"Glass"}}`.
- `session.mute` sets `Session.Muted` (the `m` of the TUI). Muted sessions get no banner, but they still become unread.
- Claude and Codex share one path, and a fixture-driven daemon test covers both. See [ADR 0016](../../docs/adr/0016-notifications-and-attention.md).
- To test by hand, put stub `osascript` and `terminal-notifier` scripts first on the `PATH` of the daemon. They log their argv, as the e2e ones in `test/e2e/testdata/bin` do. Or fire a maximum of a few real ones.

## Worktrees and PRs

See [ADR 0012](../../docs/adr/0012-worktree-detection.md). The rules are in [internal/domain/](../domain/AGENTS.md).

- **Model.** The ID of a worktree is its path. `Worktree.SessionID` is the owner (empty means unassigned), and the `Session.WorktreeIDs` of the owner lists it. `Worktree.PR` has number, state and the check rollup.
- **Scan.** One goroutine runs `git worktree list --porcelain -z` (`adapters/git.Worktrees`, a maximum of 4 at the same time). It runs from each registered repo and from the last hook `cwd` of each session, with one listing for each main checkout.
  - It runs at start, every 10 s, on `workspace.add`, and when a hook reports a new cwd or a `git worktree add`.
  - It skips a repo that git cannot read. Thus it never drops the worktrees of that repo by mistake.
  - Prunable entries (the directory is gone) count as removed.
- **Adoption.** The first scan of a repo in the life of a daemon adopts its unknown worktrees as unassigned. Stored worktrees keep their owner after restarts. `worktree.assign` sets an owner.
- **PRs.** Every 60 s, one read-only `gh api graphql` request for all repos (`adapters/github`). It matches by head branch: the open PR, else the newest.
  - It gets checks with the names of failing jobs and run URLs, review decision, unresolved threads, bot comments since the last push and mergeable state. `domain` derives merge blockers from them.
  - While checks run, the wait is a quarter of 60 s. After each failed poll, the wait doubles, to a maximum of 10x.
  - A diff goes out only when the PR changed. See [ADR 0024](../../docs/adr/0024-pr-board.md).

## Ports

`go test ./... -run Ports -tags integration -bench Ports` runs the adapter, daemon, TUI and end-to-end ports tests and `BenchmarkPortsRefresh`. Tests start their own throwaway servers and signal only process groups that they started. See [ADR 0022](../../docs/adr/0022-ports-view.md). Each worktree has `Ports`: the dev servers whose cwd is in it.

- **Read.** One goroutine refreshes through `app.ProcessTable` (`adapters/procs`) every 5 s, and only while a worktree exists.
- **Map.** The loop maps listeners to the deepest worktree that contains the cwd (`domain.PortsByWorktree`). It sends a `worktree` diff where the ports changed. Ports are never stored.
- **Kill.** `ports.kill` takes process group ids. `domain.KillGroups` keeps the groups that serve a listed port, without group 1 and the group of the daemon. `Terminate` sends SIGTERM to the group, then SIGKILL after 3 s. The port goes from the view on the next refresh.

## Cleanup

Tests have names `*Cleanup*`: `go test ./internal/domain/... -run Cleanup` and `go test -tags integration -run CleanupExec ./...`. See [ADR 0021](../../docs/adr/0021-worktree-cleanup.md). The execution is in [internal/app/](../app/AGENTS.md).

**CAUTION:** Run cleanup only against temp repos with a temp `CREW_HOME`. Do not point it at a real checkout while you test.

- **Facts.** For each worktree: one `git status`, the `origin/HEAD` lookup and `git merge-base --is-ancestor` (`adapters/git`, a maximum of 4 at the same time). Also one `lsof -d cwd` for all of them (`adapters/procs`). The daemon adds if the owner session is live, and the time of its newest event.
- **When.** Every 10 min, and when the PR poll first sees a merged PR, on a daemon goroutine (never the loop). Each action is added to `$CREW_HOME/cleanup.log`. Backups go to `$CREW_HOME/backups/`, and removed worktrees go to `$CREW_HOME/trash/`.

## Review

`go test ./internal/... -run Review -tags integration -bench Review` runs all review tests and both review benchmarks. `go test ./... -run ReviewSend -tags integration` runs the send tests, with one test that pastes into a real tmux pane. See [ADR 0023](../../docs/adr/0023-review-pane.md) and [ADR 0028](../../docs/adr/0028-review-comments-and-hunks.md).

- `review.open` runs on the connection goroutine through `app.Reviewer`, a maximum of 4 worktrees at the same time. It caches parsed diffs by base commit and tree hash. [internal/adapters/git/](../adapters/git/AGENTS.md) builds the diffs and turn snapshots.
- Each `UserPromptSubmit` queues a snapshot of the worktrees of the session to a worker. If the session owns no worktree, the snapshot is of its hook cwd. The scanner drops the turn refs of a removed worktree from its main checkout.
- Drafts are in the `review_drafts` table, and they are kept after a restart. The daemon pastes the prompt as one bracketed paste when the agent is between tools. The turn refs of the next prompt are stored on the sent draft.
- `review.comment` (`{"session","file"|"worktree"+"path","start_line","end_line","code","body"}` `"removed"` → `ReviewDraft`) adds a `ReviewComment` to the persisted draft of the session.
  - The daemon publishes it as `Diff.Comment` and the full `Diff.Draft`.
  - `State.Drafts` gives open and queued drafts to late subscribers.
  - It works without `WithReview`.

## Disk view

`go test ./internal/daemon ./internal/app ./internal/domain -run 'Disk|Reclaimable|TotalSize|RemoveWorktree|CleanupWorktree|ShellToggle'`. See [ADR 0030](../../docs/adr/0030-worktrees-disk-view.md) and [internal/tui/](../tui/AGENTS.md).

- `disk.view` returns `Cleanup.Plan` with a size for each worktree (one status check for each worktree, 4 at a time). It also returns:
  - `reclaimable`/`reclaimable_pending` and `worktrees_size`/`worktrees_pending` (`domain.Reclaimable` and `domain.TotalSize`, so clients never add sizes again)
  - `next_cleanup`: when the scheduled cleanup runs next.
- Each `disk.view` also gives the reclaimable total to the loop. The loop keeps it in `State.reclaimable` and publishes a `reclaimable` diff only when it changed. Thus the sidebar footer of the Mac app can show it.
- `cleanup.worktree` runs `app.Cleanup.RemoveWorktree`.
- `CREW_DEPS_STORE` names the shared deps store whose size the header shows. The default is the pnpm store, if there is one.

## Transcripts

`go test ./internal/daemon/ ./internal/rpc/ ./internal/app/ ./internal/domain/ -run 'Transcript|ToolCalls|NewestPage'` and `go test -tags integration ./test/integration/ -run Transcript`. The protocol is in [internal/rpc/](../rpc/AGENTS.md). See [ADR 0046](../../docs/adr/0046-remote-app.md).

`WithTranscripts(app.Transcripts, app.TranscriptWatcher)` turns on `transcript.page`, `transcript.watch` and `transcript.unwatch`. Without it, they give `unknown_method`. `Run` passes `daemon.Transcripts(fs.Transcripts{})`, which selects the Claude or Codex parser by harness.

- **Page.** The loop only finds the session. `app.Transcripts.Page` reads on the connection goroutine, backwards from `before`. It reads in windows that start at 256 KiB and double until they hold `limit` messages. It reads a maximum of 1 MiB after the page to complete its running tool calls.
- **Watch.** There is one tailer goroutine for each watched session. The first `transcript.watch` creates it. It is cancelled when its last watcher leaves (unwatch or a closed connection).
  - The loop only records the watch and queues it to the tailer, under a mutex and without blocking.
  - The tailer opens the file, answers and pushes events itself. Thus the result and the events of one watch stay in order.
  - It reads on each fsnotify change (`fs.Transcripts.Watch` watches the file, or its directory until the file exists). If the watch cannot start, it reads every second.
  - A later watcher first gets the catch-up read broadcast, then its own backlog from `after` (`TranscriptTail.Since`).
  - A tail that starts at a cursor first parses a maximum of 1 MiB before it. Thus a call whose result comes later is completed, not dropped.
- **Moves.** When the `Transcript` of an emitted session changes, the loop queues the new path to its tailer. The tailer switches files and sends `reset`. When the session is forgotten, it sends `closed`. Nothing reads a transcript while no client watches it.

## Pairing and devices

`go test ./internal/domain/ ./internal/daemon/ ./cmd/crew/ -run 'Pair|Device|Remote'`. The rules are in `domain` (`Pairing`, `NewPairCode`, `CheckDeviceToken`, `RevokeDevice`, `Device.Seen`). See [ADR 0046](../../docs/adr/0046-remote-app.md).

- **State.** Open codes and failed tries are in `state.pairing` on the loop, in memory only. A daemon restart voids open codes. Devices are in `state.devices` and the `devices` table, with ID as the key, and only the SHA-256 of the token (`TokenHash`).
- **`pair.code`** draws the code with `crypto/rand` on the connection goroutine, then issues it on the loop.
- **`pair.redeem`** draws the 32 token bytes before it goes into the loop.
  - Its `addr` is necessary, and the daemon counts it for each host. The daemon removes a port, so `serve` passes the remote address of the request as it is (or the client address that a trusted proxy forwards).
  - A wrong or expired code gives `unauthorized`. A try over the limit gives `rate_limited` and does not count as a failure.
- **`device.check`** hashes the token and compares it in constant time with the hash of each device. It records `LastSeen` a maximum of one time each minute. Thus a store write follows a maximum of one check each minute for each device.
- **Revocation.** `device.revoke` deletes the device and sends a diff to all subscribers with only `revoked_device` set. No `State` field has devices.
  - `serve` (#176) holds one `subscribe` connection, opened before it accepts requests. When the diff arrives, it closes all open streams of that device, in much less than a second.
  - The loop puts the `device.check` of a stream and a revoke in order. A check after the revoke fails. A revoke after the check gets to `serve` as a diff.
  - Device IDs are random and never used again. Thus `serve` can keep a set of revoked IDs, to close a stream whose check answered just before it read the diff.
- Tokens never go to a log or the disk in clear text. Errors never quote them, and the CLI never prints one.

## Web Push

`go test ./internal/domain/ ./internal/app/ ./internal/adapters/webpush/ ./internal/adapters/sqlite/ ./internal/daemon/ ./internal/serve/ -run Push`. `WithPush(app.PushProvider)` turns on `push.key`, `push.subscribe` and the push worker. `Run` passes `webpush.New($CREW_HOME/vapid, nil)`. See [ADR 0046](../../docs/adr/0046-remote-app.md).

- **Keys.** The adapter keeps the VAPID pair as JSON in `$CREW_HOME/vapid` (mode 600). It creates the file on the first `push.key` or send. If it cannot read the file, that is an error, and it does not change the file. To get a new pair, move the file away. Then each device must enable notifications again.
- **Subscriptions** are on the device (`Device.Push`, in the JSON of the `devices` table), one for each device.
  - `push.subscribe` replaces the subscription of the device. It also removes the endpoint from all other devices that had it (the same phone paired again).
  - `device.revoke` deletes the device, and thus its subscription.
  - `push.unsubscribe` (the sign out of the app) clears it.
- **What is sent.** `announce` gives each banner that passes mute and the coalescer to a bounded queue as `domain.PushFor(banner)`. These are the same banners that `notify.stream` carries (ADR 0016).
  - The push is `{"title","body","url","tag"}`. If there is no title, it is `crew`. If there is no body, it is the state word. Thus no push is silent.
  - `url` is `/#/sessions/<id>`, and `tag` is the session ID.
  - The terminal-in-front check of the Mac does not apply. Presence (below) replaces it.
  - A withdrawn banner is not withdrawn on the phone.
- **Presence at the terminal.** `WithPresence(app.TerminalActivity, awayAfter, every)`. `Run` passes the tmux host, `[push] away_after` from `config.toml` and 5 s. `daemon.LoadAwayAfter` reads `away_after` as a Go duration string. The default is `"2m"`, and `"0"` leaves presence out.
  - A goroutine samples `LastInput` (2 s cap) and gives the time to the loop. The loop only stores it in `domain.Presence`.
  - The owner is at the terminal while the newest input in an attached client is less than `away_after` old. No client, or a failed sample, counts as away.
  - On the loop, `domain.PushGate.Admit` holds a push while the owner is present (the newest one for each session). Else it lets the push through and drops a held push for that session.
  - After each sample, `Release` sends each held push one time if the owner is away and the session still needs them. `domain.NeedsYou` means permission or waiting, or done and unread, and not muted or ended. It forgets the other held pushes.
  - Urgent states are not exempt.
  - Held pushes are in memory. A daemon restart drops them.
  - Tests: `go test ./internal/domain/ ./internal/daemon/ -run 'Presence|PushGate|Push|Viewing'`.
- **Viewing on the phone.** `device.viewing` (`{"device","visible"}`) records, for each connection, if the app of that device is on screen. The record goes when the connection closes.
  - `serve` sends it on the own connection of the stream when the page reports `{"visible":bool}`. The page sends `true` again every 30 s while it is shown.
  - The worker skips a device with a visible report less than 90 s old (`domain.Viewing`). Thus a socket that a backgrounded app left open, or a dead link, stops counting automatically.
  - Other devices still get the push, and the push is not held for the skipped device.
- **Native clients.** `client.viewing` (`{"session","front"}`) records, for each connection, which session a native client (the Mac app, ADR 0049) shows, and if it is in front. The record goes when the connection closes.
  - Before the coalescer, `announce` skips a session that a front client shows (`domain.InView`). Thus no banner, no `notify.stream` notice and no push go out for it.
  - While a client is in front (`domain.AppFront`), `domain.Presence` counts the owner as at the terminal.
  - When the last client goes to the background or disconnects, `Presence.AppLeft` starts the `away_after` window, as typing does. The next sample releases held pushes.
  - Tests: `go test ./internal/domain/ ./internal/daemon/ -run 'Viewing|Presence|Banner'`.
- **Worker.** One goroutine reads the queue. At send time, it gets the current subscriptions from the loop. Thus a device revoked after the banner gets nothing. It calls `app.SendPush` with a 20 s cap.
  - A 404 or 410 from the push service is `app.ErrPushGone`. The worker then clears that subscription from its device and stores it, only if the device still has the same endpoint and keys. A renewed subscription stays.
  - The worker logs other failures (by host, never the endpoint URL) and does not try them again.
  - A full queue drops the push.

## Mate to mate and loop guards

`go test ./internal/domain/ ./internal/daemon/ -run 'Mate|Chain|Guard'`. See [ADR 0053](../../docs/adr/0053-mates.md).

- The adapter gives the bot user ID of each mate in its `connected` status. The worker keeps it for each running mate. An event from the bot user of a different mate on this daemon is from a mate. A mate on a different crew server counts as a person.
- A thread message from a mate that does not mention this mate (`<@bot user>`) does nothing. Thus status messages and results of a mate do not start turns. A thread message that mentions this mate counts as a mention, so it can start a session.
- Each mention or reply gets a seat: `domain.DirectMateTurn` for a person, `domain.ChainMateTurn` for a mate (from the seat of the session of that mate in the same thread). The seat goes to the new session or, through `MateTurn`, to the live session.
- `[mates]` in `config.toml` has the limits: `hop_limit` (default 5), `turns_per_hour` (default 20), `daily_turns` (default 100), `daily_tokens` (default 0, no limit) and `stop` (the global stop). 0 turns a limit off. An unknown key is a problem in `mate.list`. `mate.list` returns them as `limits`.
- Before each turn, the worker calls `domain.MateGuard` with the hops of the seat and `state.mateUsage` of the mate. Only a turn that starts a session or goes to the session adds one turn. A negative limit is a problem and the default stays. The tokens come from the status line (Claude, input plus output tokens) and the rollout (Codex, `total_token_usage`) in `Usage.Tokens`. Each increase of a mate session adds to the mate.
- When a guard stops a turn, the mate posts `domain.MateGuardText` in the thread one time. When Slack accepts the post, it does not post again for the same stop in that thread (the hop limit: for any mate in the thread) until a turn runs. A person who posts clears the hop stop.
- `mate.list` gives `guard` for each state: the stop that a new turn of a person gets now (`stopped`, `rate_limit`, `daily_turns`, `daily_tokens`, or none). The hop limit is for each thread, so it is not in it.
- `state.mateUsage` is in memory. A restart sets the counts to zero.

## Mate gate

`go test ./internal/daemon/ -run Mate`. See [ADR 0053](../../docs/adr/0053-mates.md) and the rules in [internal/domain/](../domain/AGENTS.md).

- A mate session has `Session.Mate` (`Name`, `Owner`, `Authority`). Other sessions have none, and the gate does not apply to them.
- `MateTurn{SessionID, User, Origin, Asked, Hops}` sets the seat from the Slack event that started the turn. The Slack worker posts it. Text in the thread never sets the authority. On a session that is not a mate, it does nothing.
- For each `PreToolUse` hook of a mate session, the loop uses the `action` of the hook if it is dangerous. Else it classifies the call again with `Session.Dir` as the root. Then it calls `domain.Gate` with `domain.ChainAuthority(seat, kind)`.
  - If the gate allows the action, the reply is empty. The gate marks a granted approval that it used as `used`.
  - If the gate stops the action, the reply has the deny output, and the daemon records one pending `domain.Approval` (the same pending action is not added two times).
- `approval.list` returns the pending approvals that did not expire (`domain.OpenApprovals`).

## Mate approvals

`go test ./internal/domain/ ./internal/daemon/ ./internal/adapters/sqlite/ ./internal/adapters/slack/ -run 'Approval|Gate'`. See [ADR 0053](../../docs/adr/0053-mates.md).

- **Store.** Each approval is in `state.approvals` and in the `approvals` table, so a restart keeps it. It has the mate, the session, the Slack user ID of the requester, the thread of the session, the exact `Action` (kind, tool and compact input), the time and the state: `pending`, `granted`, `denied` or `used`. A new ID is never an ID that the store has.
- **Ask.** A new pending approval does these things from the loop, with no wait:
  - It puts `domain.ApprovalPush` (`url` `/#/approvals`) in the push queue. The presence gate does not hold it.
  - With the Slack worker, a goroutine posts `domain.ApprovalRequestText` in the thread with Approve and Deny buttons (`SlackMate.PostApproval`, the action IDs `crew_approve` and `crew_deny`, the value is the approval ID). It then records the timestamp of that message in `Message`.
- **Decide.** `domain.DecideApproval(approval, by, owner, allow, now)`:
  - A Slack click counts only when the Slack user ID is the `Owner` of the mate session, and the button is from the app of the same mate. The daemon ignores other clicks and posts nothing.
  - `approval.decide` on the socket acts as the owner.
  - A decided or expired approval (more than 24 h) does not change.
- **After.** A goroutine sends `domain.ApprovalSessionPrompt` to the session through `session.send`: try the same action again, or the owner said no. It changes the Slack message to `domain.ApprovalOutcomeText` and removes the buttons.
- **Use.** The gate allows a granted approval only for the same action and the same input, and only one time, before it expires. Then its state is `used`.

## Browser

`go test ./internal/daemon/ -run Browser -bench BrowserRoundTrip`. The protocol is in [internal/rpc/](../rpc/AGENTS.md). See [ADR 0051](../../docs/adr/0051-browser-in-mac-app.md).

- `browserHub` keeps the attached connection and the calls that wait, under its own mutex. It is not loop state. Only the pane lookup of `browser.command` goes through the loop.
- `browser.command` waits on its own connection goroutine. Thus a slow page never blocks the loop or other connections.
- When the attached connection closes, or a new attach replaces it, each call that waits on it fails at once.
- The daemon keeps no browser state and never logs args or results.

### Headless fallback

`go test ./internal/daemon/ -run 'Headless|Cookie'`. The real Chromium test is in [test/integration/](../../test/integration/AGENTS.md). See [ADR 0054](../../docs/adr/0054-headless-browser-fallback.md).

- `browser.command` picks the backend with `domain.ChooseBrowser`. The inputs are the `browser` of the automation that started the session, if the Mac app is attached, and `[browser]` in `config.toml`.
- `[browser]` has `fallback` (`"headless"` or `"off"`, default `"off"`), `max_headless` (default 2) and `idle` (default `"10m"`). `config.set` and a daemon start read it again.
- `headlessPool` keeps one `app.HeadlessTab` for each session, under its own mutex. It is not loop state. A command opens the tab if necessary, under the `max_headless` limit (`domain.AdmitHeadless`).
- A reaper runs every 15 s (`WithHeadlessBrowser`). It stops the tabs of ended sessions and idle tabs (`domain.HeadlessToStop`). It never stops a tab while a command runs. When the daemon stops, it stops all tabs.
- `browser.cookies.import` merges the cookies into `$CREW_HOME/browser/profiles/default/cookies.json` (0600) and sends them to each tab that runs. A new tab gets all kept cookies.
- `browser.headless.view` takes a screenshot of a tab that runs. It does not start a tab and does not change the idle time of the tab, so the tab still stops when it is idle. The reaper does not stop the tab while the screenshot runs.
- The automation `browser` is kept in memory when the run starts its session. After a daemon restart, that session uses `any`.

## Shell and nvim

`go test ./... -run Shell -tags integration` runs the shell tests: the daemon against real tmux, and the split, popup and key pass-through of the tmux adapter. The shell and nvim tests need `tmux` and `nvim`. See [ADR 0029](../../docs/adr/0029-shell-and-nvim.md).

`daemon.WithTerminals(home, editor)` turns these methods on. They need `WithHarnesses`, and without it they give `unknown_method`. With no client layout open (the Mac app draws panes itself), they still create the pane and return it. They only skip the layout step.

- **`shell.toggle`** (`{"session","worktree","popup"}` → `{"pane","dir","shown"}`). The shell action of the disk view and `t`/`T` share this one path.
  - `session` can be empty when `worktree` names a worktree. Then the session is the owner of the worktree, or none for a worktree with no owner.
  - There is one shell for each session and worktree. It is created on first use in the path of that worktree (else the last hook cwd of the session) and kept alive.
  - The split goes below the agent pane and does not take focus. A second toggle parks it.
  - `shell.focus` (same params) shows it if necessary and puts keyboard focus in it, for `s`.
  - `popup` opens it over the attached client.
  - Panes get `CREW_SESSION` and `CREW_HOME`.
- **`nvim.toggle`** (`{"session","worktree"}` → `{"pane","socket","shown"}`) swaps the nvim of the session into the main slot, or the agent pane back. The nvim listens on `$CREW_HOME/nvim/<session>.sock`.
- **`nvim.open`** adds `{"path","line"}`. It starts nvim on the file, or tells the running nvim through `app.Editor`, then shows it. It first checks `app.Editor.Installed`. Without nvim, the daemon answers `unavailable` with how to install it.
- **Onboarding.** `onboarding.status`, `onboarding.install`, `onboarding.remove` and `onboarding.finish` run on the connection that asks, never on the loop (see [internal/adapters/onboard/](../adapters/onboard/AGENTS.md)).
