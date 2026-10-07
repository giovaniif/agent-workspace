# cmd/agentws

`main`: one binary that is the daemon, the TUI, the CLI and the hook handler. The standard library dispatches the subcommands. See [ADR 0002](../../docs/adr/0002-cli-and-ci-tooling.md).

## Startup cost

Keep startup light. Each `agentws hook` pays for package init, and `modernc.org/sqlite` init already takes about 4.5 of the 5 ms locally. `scripts/bench-hook.sh` guards the 20 ms p95 hook budget. For the same reason, do not use chroma's `lexers`/`styles` packages (see [internal/tui/](../../internal/tui/AGENTS.md)).

## The hook

Hooks call `agentws hook`. It writes one message to the socket and exits. It never waits for the daemon for more than 50 ms, and never blocks the agent. If the daemon is down, it drops the event and writes a log line to `hook.log`. It connects to the socket directly, not through `rpc.Client`. It sends one `hook` request and closes without reading the reply. The exception is an event whose stdout the harness reads (`UserPromptSubmit`). For that event, it waits a maximum of the same 50 ms, and prints nothing on timeout. See [ADR 0006](../../docs/adr/0006-hook-ingestion.md).

## Version

`-ldflags -X` stamps `internal/version.Version` and `.Commit` (`make build`, `scripts/dev`, releases). If there is no commit stamp, the commit is the VCS revision from the Go build info. The build handshake that uses them is in [internal/rpc/](../../internal/rpc/AGENTS.md). `agentws version --build` prints only `version.String()`, the exact string that the handshake compares. The Mac app reads its build from the bundled binary this way.

## Subcommands

- `agentws` attaches to the client layout. If necessary, it creates the layout and the daemon: `client.open`, then `exec` of the tmux attach argv that it returns. `agentws tui` runs in its left pane. `agentws tui --new-session` is only the new-session dialog. `n` runs it in a centred tmux popup (ADR 0037).
- `agentws daemon [start|status|stop]`: `agentws daemon` runs in the foreground. The other forms are in [internal/daemon/](../../internal/daemon/AGENTS.md).
- `agentws workspace add <path>|list|remove <path>` registers workspaces through the daemon (it starts automatically). This step is optional: `session.new` registers a folder that it does not know. The new-session dialog uses the folder where `agentws` started as its default. `list` shows `-` for the git facts of a repo until the first background refresh is done.
- `agentws new [--workspace p] [--harness claude|codex|omp] [--model m] [--effort e] <work item>` starts a session, like the `n` dialog of the TUI. Without `--workspace`, it uses the last workspace. New single-repo worktrees go under `$AGENTWS_HOME/worktrees`.
- `agentws focus <id>` focuses a session through the daemon. A `terminal-notifier` banner runs it on click.
- `agentws setup serve [--remove] [--addr host:port] [--cert file --key file | --self-signed] [--url https://host]` keeps `agentws serve` running. On Linux, it uses a systemd user unit (`agentws-serve.service`). On macOS, it uses the launchd agent `dev.agentws.serve`. The service gets the PATH and `AGENTWS_HOME` of the shell that ran the command.
  - It writes the flags in a fixed order. Thus the same arguments change nothing, and different arguments make a backup and write the file again.
  - Like `serve`, it refuses plain HTTP off loopback.
  - Tests: `go test ./... -run 'SetupServe|Systemd|Launchd'`. They put a fake `systemctl` / `launchctl` on `PATH` and use a temp `HOME`, never `~/.config/systemd` or `~/Library/LaunchAgents`. See [ADR 0046](../../docs/adr/0046-remote-app.md).
- `agentws setup daemon [--remove | --check]` keeps `agentws daemon` running. On Linux, it uses the systemd user unit `agentws-daemon.service`. On macOS, it uses the launchd agent `dev.agentws.daemon`.
  - The service gets the current `AGENTWS_HOME` and the PATH of the login shell (`$SHELL -l -c`, through `internal/adapters/loginshell`), not the PATH of the caller. Thus a daemon set up through a bare `ssh -T` still gives agent panes a full PATH.
  - The install, backup and remove rules are the same as for `setup serve`. It logs to `$AGENTWS_HOME/daemon-service.log`.
  - On Linux, if `loginctl show-user $USER -p Linger` is not `Linger=yes`, it tells you that the daemon stops at logout. It prints `sudo loginctl enable-linger $USER`.
  - `--check` prints `{"installed","running","linger"}` as JSON for the Mac app. On macOS, `linger` is always true.
  - Tests: `go test ./cmd/agentws/ -run SetupDaemon`. They put a fake `systemctl`, `launchctl`, `loginctl` and login shell on `PATH`, with a temp `HOME`. See [ADR 0049](../../docs/adr/0049-macos-app.md).
- `agentws notify stream` prints the banners and withdrawals of the daemon as JSON lines. It never starts a daemon. It waits for one, because a daemon started through the bare `ssh -T` of the bridge gives each agent pane that PATH without login.
  - `agentws notify bridge [--remote-bin path] <ssh host>` runs it through `ssh` on the Mac when the daemon is on a remote host. It posts each banner locally and connects again automatically.
  - `agentws setup bridge [--remote-bin path] [--remove] <ssh host>` keeps the bridge running as a launchd agent.
  - Tests: `go test ./... -run 'NotifyStream|NotifyRelay|NotifyBridge|Launchd|SetupBridge'`. They use a temp dir and a fake `launchctl`, never `~/Library/LaunchAgents`. See [ADR 0016](../../docs/adr/0016-notifications-and-attention.md).
- `agentws remote pair [--name n] [--url u]` asks the daemon for a pairing code. It prints a QR code, the URL `https://<url>/#pair=<code>` and the expiry.
  - The QR code uses half blocks with explicit 256-colour black on white, with the quiet zone. Thus it does not depend on the terminal theme.
  - When stdout is not a terminal or `NO_COLOR` is set, it uses plain half blocks and draws the light modules.
  - When the terminal is narrower than the code, it does not draw the code and writes a warning on stderr. The width comes from the terminal or `COLUMNS`.
  - The URL comes from `--url` or `[serve] url` in `$AGENTWS_HOME/config.toml`. If neither is set, it tells how to set one. It refuses an `http://` URL.
  - `agentws remote devices` lists paired devices. `agentws remote revoke <id>` removes one.
  - `testdata/qr.colour.golden` and `testdata/qr.plain.golden` pin the QR output (make them again with `go test ./cmd/agentws -run RemoteQR -update`). The tests decode the modules back from both outputs.
  - It is not possible to test a camera here. To verify, run `agentws remote pair` in a real terminal and scan it.
  - Tests: `go test ./cmd/agentws/ -run Remote`. See [internal/daemon/](../../internal/daemon/AGENTS.md) and ADR 0046.
- `agentws serve [--addr 127.0.0.1:7420] [--cert f --key f | --self-signed] [--url u]` runs the API of the phone app (see [internal/serve/](../../internal/serve/AGENTS.md)). It refuses plain HTTP off loopback before it starts anything. The public URL for the `Origin` check of the stream comes from `--url` or `[serve] url`. It starts the daemon like the TUI does. Tests: `go test ./cmd/agentws/ -run Serve`.
- `agentws rpc` is the one transport of the Mac app, local or through `ssh -T host agentws rpc`.
  - It connects to `$AGENTWS_HOME/agentws.sock` and copies protocol lines in both directions between the socket and stdin/stdout. The line limit is `rpc.MaxMessage` (16 MiB).
  - It never starts a daemon. If nothing listens, it writes one `{"v":1,"id":0,"error":{"code":"unavailable",...}}` line and exits 1.
  - EOF on stdin half-closes the socket. When the socket closes, it exits with status 0. An oversized line ends it with status 1.
  - The copy code is in `internal/rpcpipe`. Depguard lets it import only `rpc`, `view` and `syntax` from `internal/`, and never `os/exec`.
  - The bridge answers one method itself, `view.subscribe`. It sends `subscribe` with the same `id` and `build`, and replies with the derived state and diffs of `internal/view` (see [internal/view/](../../internal/view/AGENTS.md)).
  - It also extends `review.open`. With `"tokens": true` in the params, it passes the request on and adds `Spans`, a list of `[start, end, class]`, to each diff line of the reply (see [internal/syntax/](../../internal/syntax/AGENTS.md)). A file with no lexer gets no `Spans`.
  - All other lines pass through with no change.
  - Tests: `go test ./cmd/agentws/ -run 'Rpc|Tokens'` and `go test -tags integration ./test/integration/ -run RpcBridge`. See [ADR 0049](../../docs/adr/0049-macos-app.md).
- `agentws worktree list|assign <path> <session>` prints the path, branch, owner and PR of each worktree, and sets an owner.
- `agentws cleanup [--dry-run]` runs (or only prints) the cleanup plan through the daemon (see [internal/daemon/](../../internal/daemon/AGENTS.md)).
- `agentws pr <session id or name> [--json]` prints the PR board of a session from daemon state. `testdata/pr.json.golden` pins the JSON shape. To make it again, run `go test ./cmd/agentws -run PRBoardJSON -update` and review the diff. [ADR 0024](../../docs/adr/0024-pr-board.md) documents the shape. Run the board tests with `go test ./... -run PRBoard`. The tests of the github adapter and the integration tests use a fake `gh` script, so nothing goes to GitHub.
- `agentws review comment [--session id] (--file abs | --worktree id --path rel) --start n [--end n] [--code text] --body text` adds a draft comment. `agentws review scope [--session id] [--scope s]` prints the path, base commit and files of each worktree as JSON. `agentws review send [--session id]` sends the draft, or queues it until the agent is between tools. The default of `--session` is `$AGENTWS_SESSION`, which the shell and nvim panes have.
- `agentws setup claude|codex|omp [--remove]`: see [internal/adapters/](../../internal/adapters/AGENTS.md). `agentws setup` with no arguments and `agentws setup nvim [--remove]`: see [internal/adapters/onboard/](../../internal/adapters/onboard/AGENTS.md).
- `agentws setup-worktree <path>`: see [internal/adapters/setup/](../../internal/adapters/setup/AGENTS.md).
- `agentws statusline` runs the status line of the user, then reports model, effort, context left and rate limits.
- `agentws debug seed N [--codex]` adds N fake Claude sessions, to try the TUI.
  - There are two sessions for each task, with one to three worktrees each, event logs and fresh limits. The Claude 5h window is 88% used, so the red state shows.
  - `--codex` makes every third session Codex, with Codex limits.
  - The first seeded session also gets three subagents: one is nested, two are running. Its PR is open with failing checks, so `agentws pr seed-session-1` shows a PR board.
- `agentws debug session [--once] <id>` prints the state, harness, pane, model, effort, context left and limit used of a session. Then it prints each change until you interrupt it. `--once` prints only the current line.
- `agentws debug launch --harness claude --dir <dir> [--model m] [--effort e]` opens a harness pane in any dir, with no task or worktree.
