# Architecture

This covers how `agentws` is built, at the top level. What it does is in [FEATURES.md](FEATURES.md). Each subsystem's detail lives in the `AGENTS.md` of the directory that owns it (indexed under [Subsystems](#subsystems)); rules for contributors are in [AGENTS.md](AGENTS.md).

## Stack

- **Language:** Go (current stable). It builds one static binary that is the daemon, the TUI, the CLI and the hook handler. The reasons are in [docs/adr/0001-go.md](docs/adr/0001-go.md).
- **TUI:** Bubble Tea v2, Lip Gloss and Bubbles (charmbracelet).
- **Syntax highlighting:** chroma's lexer engine with a curated set of its lexers in `internal/syntax/lexers/`, not its `lexers`/`styles` packages, whose init would slow every hook.
- **Git:** the `git` CLI via exec, never a Go git library.
- **GitHub:** the `gh` CLI (`gh api graphql`), which reuses the user's `gh` auth. One GraphQL request per poll, with backoff.
- **Terminals:** the `tmux` CLI against a dedicated server (`tmux -L agentws`) with its own config, driven only by `internal/adapters/tmux`. See [docs/adr/0003-tmux-terminal-host.md](docs/adr/0003-tmux-terminal-host.md).
- **Config:** `github.com/BurntSushi/toml` reads the per-repo `.agentws.toml` setup recipe and `$AGENTWS_HOME/config.toml`.
- **Storage:** SQLite via `modernc.org/sqlite` (no cgo), with embedded migrations and write-behind, at `$AGENTWS_HOME/state.db` (default `~/.agentws`). See [docs/adr/0004-sqlite-store.md](docs/adr/0004-sqlite-store.md).
- **IPC:** a Unix socket at `$AGENTWS_HOME/agentws.sock` carrying newline-delimited JSON: request/response calls plus a subscribe stream for state updates. See [docs/adr/0005-daemon-rpc.md](docs/adr/0005-daemon-rpc.md).
- **Notifications:** `terminal-notifier` when on `PATH`, else `osascript`. See [docs/adr/0016-notifications-and-attention.md](docs/adr/0016-notifications-and-attention.md).
- **nvim:** a small Lua plugin in `nvim/` that talks to the daemon through `agentws` CLI calls, and a long-lived nvim per session that the daemon drives over `nvim --listen`. See [docs/adr/0029-shell-and-nvim.md](docs/adr/0029-shell-and-nvim.md).
- **Tooling:** `go test`, `golangci-lint`, `testscript` for the e2e suite, `gremlins` for mutation testing, a `tdd` CI job that runs new tests against the base branch, `scripts/lint-comments`, `scripts/lint-agents`, GitHub Actions on macOS, goreleaser.

## Layers

```
cmd/agentws            main: subcommands (daemon, tui, hook, setup, new, cleanup, ...)
internal/domain        pure types and rules. No IO, no imports from other internal packages.
internal/app           use cases + ports (interfaces the use cases need)
internal/adapters/...  tmux, git, github, linear, claude, codex, sqlite, notify, procs (lsof/ports), nvim, fs, setup, onboard
internal/daemon        socket server, event loop, workers; wires adapters into app
internal/rpc           protocol types + client (used by tui, hook, cli, nvim)
internal/tui           Bubble Tea models; talks only to rpc.Client
internal/serve         agentws serve: HTTP and WebSocket API for the phone app; talks only to rpc
nvim/                  Lua plugin
scripts/               repo tooling: lint-comments, lint-agents, tdd-check, mutate, bench-hook, dev
test/e2e               testscript suite with fake claude, codex and gh
test/integration       the daemon with its real adapters
```

Dependency rule: `domain` ← `app` ← `adapters`/`daemon`, and `tui` → `rpc` only, as does `serve`. `golangci-lint depguard` enforces it (rules in `.golangci.yml`), so breaking it fails CI. `tui` may import `domain` types.

- **Domain** holds every rule as a pure, table-tested function: the session state machine (`Session.Apply`), naming, banners, cleanup decisions, quotas and fallback, discovery, worktree attribution, ports, disk and review.
- **App** holds the use cases and the port interfaces the adapters implement; its tests use in-memory fakes.

## Process model and data flow

- **Daemon.** One long-lived `agentws daemon` per `AGENTWS_HOME`, guarded by a `flock`. A single goroutine event loop owns all in-memory state; SQLite is written in the background after each change, and reads never hit disk. Git, gh, tmux, `lsof` and `du` run on workers or connection goroutines, never the loop.
- **Hooks in.** Claude and Codex hooks run `agentws hook`, which writes one `hook` message to the socket and exits. The daemon maps the pane to a session, the hook to a harness-neutral event, applies `Session.Apply` and performs its effects (banners, queued sends).
- **State out.** Every change bumps `seq` and fans a diff out to subscribers. The TUI subscribes, keeps a snapshot and renders from it; a second connection makes calls so diffs never delay a key.
- **Panes.** The tmux server and the daemon own the sessions' panes, so quitting the TUI changes nothing. `agentws` attaches to a client layout: the TUI on the left, the session in view swapped into the main slot.
- **Workers.** Worktree scan (10 s), PR poll (60 s, one GraphQL request), ports (5 s), cleanup (10 min and on merge), workspace facts (30 s), title strips (250 ms), turn snapshots per prompt, title resolution per new session.

## Subsystems

| Subsystem | Where |
|---|---|
| CLI, hook process, startup cost | [cmd/agentws/AGENTS.md](cmd/agentws/AGENTS.md) |
| Protocol, methods, build handshake, client | [internal/rpc/AGENTS.md](internal/rpc/AGENTS.md) |
| Daemon process, event loop, workspaces, sessions, naming, launcher, fallback, attention, worktrees and PRs, ports, cleanup schedule, review sends, shell and nvim | [internal/daemon/AGENTS.md](internal/daemon/AGENTS.md) |
| Domain rules: state machine, discovery, attribution, review | [internal/domain/AGENTS.md](internal/domain/AGENTS.md) |
| Use cases, ports, cleanup execution, disk sizes | [internal/app/AGENTS.md](internal/app/AGENTS.md) |
| Sidebar, limits bar, theme, review viewer, disk view | [internal/tui/AGENTS.md](internal/tui/AGENTS.md) |
| Remote API for the phone app (`agentws serve`) | [internal/serve/AGENTS.md](internal/serve/AGENTS.md) |
| Harness adapters, notify, procs, fs, github, linear, sqlite | [internal/adapters/AGENTS.md](internal/adapters/AGENTS.md) |
| Repo facts, worktree listing, review diffs, turns, hunks | [internal/adapters/git/AGENTS.md](internal/adapters/git/AGENTS.md) |
| tmux server and keys | [internal/adapters/tmux/AGENTS.md](internal/adapters/tmux/AGENTS.md) |
| Worktree setup recipes | [internal/adapters/setup/AGENTS.md](internal/adapters/setup/AGENTS.md) |
| First-run walkthrough | [internal/adapters/onboard/AGENTS.md](internal/adapters/onboard/AGENTS.md) |
| nvim plugin | [nvim/AGENTS.md](nvim/AGENTS.md) |

## Staying fast

Clean layers must not cost latency, so these rules apply:

- **One state owner.** A single goroutine event loop in the daemon owns in-memory state. SQLite is written in the background after each change. Reads never hit disk.
- **Push, don't poll, for agent state.** Hooks call `agentws hook`, which writes one message to the socket and exits within 50 ms, never blocking the agent; if the daemon is down the event is dropped and logged to `hook.log`. `scripts/bench-hook.sh` checks the wall-time budget in CI. See [docs/adr/0006-hook-ingestion.md](docs/adr/0006-hook-ingestion.md).
- **TUI renders from a snapshot.** The daemon pushes state diffs, and the TUI never runs git, gh or tmux on the render path.
- **Heavy work goes to workers:** `du`, cleanup, diffs, and PR polling run in a bounded pool with debounce. Diffs are cached by tree hash.
- **Batch git:** one `git status --porcelain=v2 -z` per worktree per change burst, triggered by fsnotify with a 300 ms debounce.
- **Poll GitHub politely:** one GraphQL request per poll for every repo, 60 s base interval, faster only while checks are running, exponential backoff on failure. GraphQL POSTs cannot use ETags (ADR 0024).

**Budgets** (CI benchmarks enforce these where possible):

| Path | Budget |
|---|---|
| `agentws hook` process wall time | < 20 ms p95 |
| Hook event → sidebar updated | < 150 ms |
| Keypress → frame | < 16 ms |
| Switch session (swap pane) | < 60 ms |
| Open review for a 50-file diff | < 300 ms |
| Daemon idle CPU | < 0.5% |
| Ports refresh (netstat + lsof) | < 50 ms, at most every 5 s |
| `session.new` + `session.focus` (no recipe) | < 1 s |
| Discovery over 15 repos | < 300 ms |
| Clone a 1 GB `node_modules` | < 5 s, < 50 MB |
