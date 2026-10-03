# internal/adapters

Implementations of the `app` ports. This is where exec, disk and network live. git, tmux, setup recipes and onboarding have their own notes: [git/](git/AGENTS.md), [tmux/](tmux/AGENTS.md), [setup/](setup/AGENTS.md), [onboard/](onboard/AGENTS.md).

## Harness adapters

`app.HarnessAdapter` turns an `app.LaunchRequest` into the `PaneSpec` that runs the harness.

- **Claude** (`claude`): `agentws setup claude [--remove]` merges agentws hooks and the status-line wrapper into `$CLAUDE_CONFIG_DIR/settings.json` (default `~/.claude`), backs it up, and undoes it. `agentws statusline` chains the user's own status line and reports model, effort, context left and rate limits. Tests set `CLAUDE_CONFIG_DIR` to a temp dir and never touch the real `~/.claude`. See [ADR 0011](../../docs/adr/0011-claude-harness-adapter.md).
- **Codex** (`codex`): the `agentws setup codex [--remove]` merge into `$CODEX_HOME/hooks.json`, hook and notify payload parsing, the rollout reader that supplies model, effort, context and limits, pane lookup, and launching (`codex.Adapter` is registered beside Claude's, so `session.launch --harness codex` works). Tests of it, and of anything else that touches Codex config, use a temp `CODEX_HOME`; never the real `~/.codex`. The mapping, formulas and where each number comes from are in [codex/README.md](codex/README.md) and [ADR 0010](../../docs/adr/0010-codex-adapter.md).
- **omp** (`omp`): `agentws setup omp [--remove]` writes one file, `$PI_CODING_AGENT_DIR/hooks/post/agentws.ts` (default `~/.omp/agent`). Its first line is the ownership marker; a file without it is reported, never overwritten or removed. The hook names come from the domain harness table, and the hook sends `model` and `effort` on its JSON, which the daemon reports as status. Launching builds `omp [--resume id] [--model m] [--thinking e] -- <prompt>`. The TUI draws from `$AGENTWS_HOME/cache/omp-models.json` immediately, then a command runs `omp models --json` and replaces the cache and the open list. With no catalog the model stays a text field. omp has no limits row and no session-file reader. Tests use a temp `PI_CODING_AGENT_DIR`; `go test ./internal/adapters/omp` also runs the hook file under `bun` when it is installed.
- `claude.Installed` and `codex.Installed` run Setup's merge in memory and report whether it would change nothing, so the walkthrough and the CLI agree.

## Others

- **notify**: `terminal-notifier` when it is on `PATH` at daemon start (grouped per session, click runs `agentws focus <id>`, and a session's banner is withdrawn once it resumes or is focused), else `osascript`; with both, `Fallback` posts through `osascript` when `terminal-notifier` fails (macOS often has its notifications off after install), and with neither, `Silent`. `Relay` posts a remote daemon's banners for `agentws notify bridge`. Both implement `app.Notifier`; `osascript` also implements `app.Foreground`. See [ADR 0016](../../docs/adr/0016-notifications-and-attention.md).
- **launchd**: writes and loads (or unloads and deletes) the launchd agent behind `agentws setup bridge`; tests use a temp dir and a fake runner.
- **procs** (`app.ProcessTable`): `netstat -anv -p tcp` for listening sockets, then one `lsof -a -d cwd -p <pids>` for their group, command and cwd. A refresh (both commands) must cost under 50 ms; `BenchmarkPortsRefresh` fails above that (measured 19 ms). Also one `lsof -d cwd` for cleanup's holders check.
- **fs**: `WorkspaceFS` (stat and readdir only, no git), `Du` (`du -sk -P`; `go test -tags integration -run Disk ./internal/adapters/fs/`), the trash, and `AuditLog`.
- **github**: one read-only `gh api graphql` request per poll for all repos; tests use a fake `gh`, nothing reaches GitHub. GraphQL POSTs cannot use ETags ([ADR 0024](../../docs/adr/0024-pr-board.md)). Also `gh pr view` titles for naming.
- **linear**: issue titles over the Linear API, tested against a mocked API.
- **sqlite**: `modernc.org/sqlite` (no cgo), embedded migrations, write-behind; `$AGENTWS_HOME/state.db`. See [ADR 0004](../../docs/adr/0004-sqlite-store.md).
- **loginshell**: `Path` runs `$SHELL -l -c` (else `/bin/sh`) and reads its PATH after a marker, so profile output is ignored. Tests use a temp `HOME` with its own `.profile`: `go test ./internal/adapters/loginshell/`.
- **nvim**: drives each session's nvim over `nvim --listen`; `Installed` is `exec.LookPath`.
