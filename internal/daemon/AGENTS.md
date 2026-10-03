# internal/daemon

Socket server, event loop and workers; wires the adapters into `app`. It may not exec: tests here use fakes, and `test/integration` runs it with real adapters. Only this package may import `internal/adapters/tmux`. The wire protocol is in [internal/rpc/](../rpc/AGENTS.md).

## Process

`agentws daemon` runs in the foreground until SIGINT/SIGTERM. `agentws daemon start` spawns it detached (`setsid`, output to `$AGENTWS_HOME/daemon.log`) and waits for the socket. `daemon status` prints pid, uptime, and session and worktree counts, or exits 1 with `not running` (it never starts one); `daemon stop` sends SIGTERM and waits for the pid file to go. On start, before anything else, the daemon appends the login shell's PATH entries it lacks to its own (`loginshell.Path` with a 1 s cap, `domain.MergeLoginPath`; a failure is logged and PATH left alone). tmux gives each new pane the daemon's PATH, and whatever started the daemon (a bare `ssh`, launchd, systemd) may lack `~/.local/bin` and the like (#153). The e2e `login_path.txtar` covers it. Files in `$AGENTWS_HOME` (default `~/.agentws`):

| File | Purpose |
|---|---|
| `agentws.lock` | `flock` held for the daemon's life; the kernel drops it if the daemon dies. A second daemon exits 1 with `already running (pid N)`. |
| `agentws.pid` | written after taking the lock, removed on clean exit |
| `agentws.sock` | the socket, mode 0600; a stale one is removed by the next lock holder |
| `state.db` | the SQLite store; loaded once on start |

**Event loop.** One goroutine owns all state. Adapters call `Daemon.Post(event)` with `WorkspaceChanged`, `TaskChanged`, `WorktreeChanged` or `SessionChanged`; each one replaces the entity by key (`WorkspaceRemoved` deletes it), enqueues a store write, bumps `seq` and fans the diff out to every subscriber in order. Connections read state only through closures run on the loop. Each connection has a 1024-message outbox; one that falls behind is disconnected, never waited on. Hooks are parsed on the loop (a small JSON decode); git, gh, tmux and `du` never run on it.

## Workspaces

See [ADR 0007](../../docs/adr/0007-workspace-discovery.md); the discovery rules are in [internal/domain/](../domain/AGENTS.md). `workspace.add` answers from the filesystem alone, publishes the workspace, then refreshes repo facts off the loop and publishes again only if something changed. The daemon repeats that for every workspace every 30 s. `Workspace.LastUsed` is set on every `add`, stored with the workspace, and `workspace.list` returns the most recent as `last_used`; the new-session dialog defaults to it.

## Sessions

See [ADR 0015](../../docs/adr/0015-session-lifecycle.md).

- **Start.** `n` in the TUI or `agentws new` calls `session.new`. The work item is a Linear issue URL, a GitHub PR URL, or text; a URL of neither shape stays text (`domain.ParseWorkItem`). A single repo gets one worktree at `$AGENTWS_HOME/worktrees/<repo>/<slug>` branched from `origin/<default>` and its setup recipe run; an orchestration root starts at the root with no worktree (`domain.PlanSessionStart`). The work item is the agent's first prompt.
- **Budget.** `session.new` plus `session.focus`, excluding the setup recipe, must finish in < 1 s; the `NewSession` integration test (`go test -tags integration -run NewSession ./internal/daemon/`, real temp repos, a fake harness script and its own tmux socket) checks it.
- **Focus.** `enter`, and every new session, calls `session.focus`, which swaps the pane into the main slot, sets `Session.Focused` and clears unread. Focus is cleared on daemon start.
- **End.** `x` then `y` calls `session.end`: the pane is killed and the session goes `idle` with no pane and `Ended`, which takes it off the sidebar. It stays in the state, hidden, until its last worktree is removed, then it and its events are deleted (`Session.Forgotten`); one with no worktree is deleted at once. See [ADR 0035](../../docs/adr/0035-ended-sessions.md). If it was the session in view, the next session in sidebar order (`domain.NextInView`, picked before the end is published) is shown, or an empty-state pane when none is left. An agent that exits by itself is handled the same way: a 2 s worker notices the slot lost its pane and ends the session in view.
- **Resume.** Every hook's `session_id` (Codex notify: `thread-id`) is kept as `Session.ResumeID`, the newest winning (Claude's `/clear` starts a new one), and `Session.Dir` is the dir the agent was launched in. `session.resume` (`{"id"}`) relaunches an ended session there with `claude --resume <id>`, `codex resume <id>`, or `omp --resume <id>`, same model and effort, and keeps its ID, task and worktrees. A live session, or one missing either field, is a bad request. See [ADR 0042](../../docs/adr/0042-resume-ended-sessions.md).
- **Title strips.** Every pane has a top border; an agent pane's shows `domain.AgentTitle` (state, harness, model, effort, cwd, then a tab per worktree with its PR), set through `TerminalHost.SetTitle` by a 250 ms worker that copies state on the loop only when it changed, and formats and runs tmux off it, only for changed titles. See [ADR 0038](../../docs/adr/0038-pane-title-strips.md).
- **Survival.** The daemon and the tmux server own sessions, so quitting the TUI or detaching changes nothing. On daemon start, restored sessions whose pane is gone are ended (`app.ReconcilePanes`), off the loop.
- **Model and effort.** `M`/`E` in the TUI send `session.switch`. The daemon types `/model <x>` or `/effort <y>` into a Claude pane once the agent is between tools; for Codex it types `/model` and walks the picker with arrow keys ([ADR 0034](../../docs/adr/0034-codex-model-picker-switching.md), integration test in [test/integration/](../../test/integration/AGENTS.md)); for omp it types `/switch <id>` or `/switch <model>:<level>`. Tests fake the terminal host; by hand, run the TUI against a fake `claude` on `PATH`. See [ADR 0018](../../docs/adr/0018-model-effort-switching.md).
- **Harnesses.** A Codex hook's model is applied at once; the rollout is read in a worker, never on the loop. An omp hook carries `model` and `effort` and is reported on the loop, with no session-file read (see [internal/adapters/](../adapters/AGENTS.md)).

## Naming

`go test ./... -run Naming` runs every naming test (domain rules, resolvers, the Linear and `gh` adapters against a mocked API and a fake `gh`, the daemon and the TUI). The name is the work item's, resolved off the loop: a worker asks `app.TitleResolvers` (Linear API with the `[linear] token = "..."` from `$AGENTWS_HOME/config.toml`, then `gh pr view`) after `session.new` and merges the title into the task. Tests never read the token. In the TUI, `R` calls `session.rename` (`{"id","name"}`) to rename and pin, `A` calls `session.unpin` (`{"id"}`). See [ADR 0026](../../docs/adr/0026-session-naming.md).

## Launcher

`go test ./... -run Launcher -tags integration` runs the domain, daemon and TUI launcher tests and the integration test (real repos and tmux, a fake Linear server; nothing reaches Linear). `L` calls `launcher.enqueue` (`{"workspace","input","harness","model","effort"}` → `{"queued","rejected"}`) with the Linear URLs in the input; `launcher.drop` (`{"id"}`) and `launcher.retarget` (`{"id","harness","model","effort"}`) edit a waiting item (`c` moves queued issues to Codex when offered, `X` clears the queue). The daemon holds the queue (`State.Queue`, `Diff.Queue`), drains it on a worker by `domain.DrainLauncher` with `[launcher] max_parallel` in `$AGENTWS_HOME/config.toml` (default 3) and starts each item through the `session.new` path; the sidebar lists it with the `OfferFallbacks` offer. See [ADR 0031](../../docs/adr/0031-linear-launcher.md).

## Codex fallback

`go test ./... -run Fallback`. `[fallback]` in `$AGENTWS_HOME/config.toml` sets `threshold` and the `models`/`efforts` maps from Claude to Codex. Under the harness row the new-session dialog shows `domain.Advise`'s low-quota warning; `ctrl+s` takes the offer (switches to the other harness when it has reported limits). See [ADR 0027](../../docs/adr/0027-codex-fallback.md).

## Attention and banners

`go test ./... -run Notify` runs every notification test. The daemon performs the effects `Session.Apply` returns. `EffectNotify` becomes a `domain.Banner` on the loop (`BannerFor`, then `Coalescer`), with no IO. A worker reads a bounded queue and calls `app.Notifier`; for a focused session it first asks `app.Foreground` whether a terminal app is in front, and drops the banner if so. The adapters are in [internal/adapters/](../adapters/AGENTS.md).

- Title is the session name (`NameFor`, else the harness), body is `needs permission`, `waiting` or `done`. At most one banner per session per 10 s.
- `$AGENTWS_HOME/notify.json` sets an optional macOS sound per event: `{"sounds":{"permission":"Glass"}}`.
- `session.mute` sets `Session.Muted` (the TUI's `m`). Muted sessions get no banner and still go unread.
- Claude and Codex share one path, and a fixture-driven daemon test covers both. See [ADR 0016](../../docs/adr/0016-notifications-and-attention.md).
- By hand, put stub `osascript` and `terminal-notifier` scripts first on `PATH` for the daemon (they log their argv; the e2e ones in `test/e2e/testdata/bin` do), or fire at most a couple of real ones.

## Worktrees and PRs

See [ADR 0012](../../docs/adr/0012-worktree-detection.md); the rules are in [internal/domain/](../domain/AGENTS.md).

- **Model.** A worktree's ID is its path. `Worktree.SessionID` is the owner (empty is unassigned) and the owner's `Session.WorktreeIDs` lists it. `Worktree.PR` carries number, state and the check rollup.
- **Scan.** One goroutine runs `git worktree list --porcelain -z` (`adapters/git.Worktrees`, at most 4 in flight) from every registered repo and every session's last hook `cwd`, one listing per main checkout. It runs at start, every 10 s, on `workspace.add`, and when a hook reports a new cwd or a `git worktree add`. A repo git cannot read is skipped, so its worktrees are never dropped by mistake. Prunable entries (directory gone) count as removed.
- **Adoption.** The first scan of a repo in a daemon's life adopts its unknown worktrees as unassigned; stored worktrees keep their owner across restarts. `worktree.assign` sets an owner.
- **PRs.** Every 60 s, one read-only `gh api graphql` request for all repos (`adapters/github`), matched by head branch: the open PR, else the newest. It carries checks with failing job names and run URLs, review decision, unresolved threads, bot comments since the last push and mergeable state; `domain` derives merge blockers from them. The wait is a quarter of that while checks run and doubles per failed poll up to 10x. A diff goes out only when the PR changed. See [ADR 0024](../../docs/adr/0024-pr-board.md).

## Ports

`go test ./... -run Ports -tags integration -bench Ports` runs the adapter, daemon, TUI and end-to-end ports tests plus `BenchmarkPortsRefresh`. Tests start their own throwaway servers and only signal process groups they started. See [ADR 0022](../../docs/adr/0022-ports-view.md). Each worktree carries `Ports`: the dev servers whose cwd is inside it.

- **Read.** One goroutine refreshes through `app.ProcessTable` (`adapters/procs`) every 5 s, and only while a worktree exists.
- **Map.** The loop maps listeners to the deepest worktree containing the cwd (`domain.PortsByWorktree`) and emits a `worktree` diff where the ports changed. Ports are never stored.
- **Kill.** `ports.kill` takes process group ids. `domain.KillGroups` keeps those that serve a listed port, minus group 1 and the daemon's own; `Terminate` sends SIGTERM to the group, then SIGKILL after 3 s. The port leaves the view on the next refresh.

## Cleanup

Tests are named `*Cleanup*`: `go test ./internal/domain/... -run Cleanup` and `go test -tags integration -run CleanupExec ./...`. Only ever run cleanup against temp repos with a temp `AGENTWS_HOME`; never point it at a real checkout while testing. See [ADR 0021](../../docs/adr/0021-worktree-cleanup.md); execution is in [internal/app/](../app/AGENTS.md).

- **Facts.** Per worktree: one `git status`, the `origin/HEAD` lookup and `git merge-base --is-ancestor` (`adapters/git`, at most 4 in flight), plus one `lsof -d cwd` for all of them (`adapters/procs`). The daemon adds whether the owning session is live and its newest event time.
- **When.** Every 10 min and when the PR poll first sees a PR merged, on a daemon goroutine (never the loop). Each action is appended to `$AGENTWS_HOME/cleanup.log`; backups go to `$AGENTWS_HOME/backups/`, removed worktrees to `$AGENTWS_HOME/trash/`.

## Review

`go test ./internal/... -run Review -tags integration -bench Review` runs every review test and both review benchmarks; `go test ./... -run ReviewSend -tags integration` runs the send tests, including one that pastes into a real tmux pane. See [ADR 0023](../../docs/adr/0023-review-pane.md) and [ADR 0028](../../docs/adr/0028-review-comments-and-hunks.md).

- `review.open` runs on the connection goroutine through `app.Reviewer`, at most 4 worktrees at once, and caches parsed diffs by base commit and tree hash. Diffs and turn snapshots are built by [internal/adapters/git/](../adapters/git/AGENTS.md).
- Each `UserPromptSubmit` queues a snapshot of the session's worktrees (or its hook cwd when it owns none) to a worker. The scanner drops a removed worktree's turn refs from its main checkout.
- Drafts live in the `review_drafts` table and survive a restart. The prompt is pasted as one bracketed paste once the agent is between tools. The next prompt's turn refs are stored on the sent draft.
- `review.comment` (`{"session","file"|"worktree"+"path","start_line","end_line","code","body"}` `"removed"` → `ReviewDraft`) adds a `ReviewComment` to the session's persisted draft. The daemon publishes it as `Diff.Comment` plus the whole `Diff.Draft`, and `State.Drafts` carries open and queued drafts to late subscribers. It works without `WithReview`.

## Disk view

`go test ./internal/daemon ./internal/app ./internal/domain -run 'Disk|Reclaimable|TotalSize|RemoveWorktree|CleanupWorktree|ShellToggle'`. `disk.view` returns `Cleanup.Plan` with a size per worktree (one status check per worktree, 4 at a time); `cleanup.worktree` runs `app.Cleanup.RemoveWorktree`. `AGENTWS_DEPS_STORE` names the shared deps store whose size the header shows (default: pnpm's store if present). See [ADR 0030](../../docs/adr/0030-worktrees-disk-view.md) and [internal/tui/](../tui/AGENTS.md).

## Shell and nvim

`go test ./... -run Shell -tags integration` runs the shell tests (daemon against real tmux, the tmux adapter's split, popup and key pass-through); they and the nvim ones need `tmux` and `nvim`. `daemon.WithTerminals(home, editor)` turns these methods on; they need `WithHarnesses` and a client host, and answer `unknown_method` without them. See [ADR 0029](../../docs/adr/0029-shell-and-nvim.md).

- **`shell.toggle`** (`{"session","worktree","popup"}` → `{"pane","dir","shown"}`). The disk view's shell action and `t`/`T` share this one path: `session` may be empty when `worktree` names one, and then the worktree's owner is the session, or none for an unowned worktree. One shell per session and worktree, created on first use in that worktree's path (else the session's last hook cwd) and kept alive. The split goes below the agent pane without taking focus; a second toggle parks it. `shell.focus` (same params) shows it if needed and puts keyboard focus in it, for `s`. `popup` opens it over the attached client instead. Panes get `AGENTWS_SESSION` and `AGENTWS_HOME`.
- **`nvim.toggle`** (`{"session","worktree"}` → `{"pane","socket","shown"}`) swaps the session's nvim, listening on `$AGENTWS_HOME/nvim/<session>.sock`, into the main slot or the agent pane back. **`nvim.open`** adds `{"path","line"}`: it starts nvim on the file, or tells the running one through `app.Editor`, then shows it. `app.Editor.Installed` is checked first; without nvim the daemon answers `unavailable` with how to install it.
- **Onboarding.** `onboarding.status`, `onboarding.install` and `onboarding.finish` run on the asking connection, never on the loop (see [internal/adapters/onboard/](../adapters/onboard/AGENTS.md)).
