# Architecture

This document gives the top-level design of `agentws`. [FEATURES.md](FEATURES.md) tells what it does. The details of each subsystem are in the `AGENTS.md` of the directory that owns it (see [Subsystems](#subsystems)). The rules for contributors are in [AGENTS.md](AGENTS.md).

## Stack

- **Language:** Go (current stable). It builds one static binary. The binary is the daemon, the TUI, the CLI and the hook handler. The reasons are in [docs/adr/0001-go.md](docs/adr/0001-go.md).
- **TUI:** Bubble Tea v2, Lip Gloss and Bubbles (charmbracelet).
- **Syntax highlighting:** chroma's lexer engine with a curated set of its lexers in `internal/syntax/lexers/`. Do not use its `lexers`/`styles` packages: their init makes each hook slower.
- **Git:** the `git` CLI through exec. Do not use a Go git library.
- **GitHub:** the `gh` CLI (`gh api graphql`). It uses the user's `gh` auth. Each poll is one GraphQL request, with backoff.
- **Terminals:** the `tmux` CLI on a dedicated server (`tmux -L agentws`) with its own config. Only `internal/adapters/tmux` controls it. See [docs/adr/0003-tmux-terminal-host.md](docs/adr/0003-tmux-terminal-host.md).
- **Config:** `github.com/BurntSushi/toml` reads the per-repo `.agentws.toml` setup recipe and `$AGENTWS_HOME/config.toml`.
- **Storage:** SQLite through `modernc.org/sqlite` (no cgo), with embedded migrations and write-behind. The file is `$AGENTWS_HOME/state.db` (default `~/.agentws`). See [docs/adr/0004-sqlite-store.md](docs/adr/0004-sqlite-store.md).
- **IPC:** a Unix socket at `$AGENTWS_HOME/agentws.sock`. It carries newline-delimited JSON: request/response calls and a subscribe stream for state updates. See [docs/adr/0005-daemon-rpc.md](docs/adr/0005-daemon-rpc.md).
- **Notifications:** `terminal-notifier` if it is on `PATH`, else `osascript`. See [docs/adr/0016-notifications-and-attention.md](docs/adr/0016-notifications-and-attention.md).
- **nvim:** a small Lua plugin in `nvim/` talks to the daemon through `agentws` CLI calls. Each session has a long-lived nvim that the daemon controls through `nvim --listen`. See [docs/adr/0029-shell-and-nvim.md](docs/adr/0029-shell-and-nvim.md).
- **Tooling:**
  - `go test` and `golangci-lint`.
  - `testscript` for the e2e suite, and `gremlins` for mutation testing.
  - A `tdd` CI job that runs new tests against the base branch.
  - `scripts/lint-comments` and `scripts/lint-agents`.
  - GitHub Actions on macOS, and goreleaser.

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

Dependency rule: `domain` ← `app` ← `adapters`/`daemon`. `tui` and `serve` talk to the daemon only through `rpc`. `tui` can also import `domain` types. `golangci-lint depguard` enforces the rule, and `.golangci.yml` lists the exact allowed imports. Thus a break of the rule fails CI.

- **Domain** holds each rule as a pure, table-tested function. The rules are the session state machine (`Session.Apply`), naming, banners, cleanup decisions, quotas and fallback, discovery, worktree attribution, ports, disk and review.
- **App** holds the use cases and the port interfaces that the adapters implement. Its tests use in-memory fakes.

## Process model and data flow

- **Daemon.** There is one long-lived `agentws daemon` for each `AGENTWS_HOME`. A `flock` guards it. One goroutine event loop owns all in-memory state. The daemon writes SQLite in the background after each change, and reads never go to disk. Git, gh, tmux, `lsof` and `du` run on workers or connection goroutines, never on the loop.
- **Hooks in.** Claude and Codex hooks run `agentws hook`. It writes one `hook` message to the socket and exits. The daemon maps the pane to a session and the hook to a harness-neutral event. Then it applies `Session.Apply` and does its effects (banners, queued sends).
- **State out.** Each change increments `seq` and sends a diff to all subscribers. The TUI subscribes, keeps a snapshot and renders from it. A second connection makes the calls, so diffs never delay a key.
- **Panes.** The tmux server and the daemon own the panes of the sessions. Thus, when you quit the TUI, nothing changes. `agentws` attaches to a client layout: the TUI is on the left, and the daemon swaps the session in view into the main slot.
- **Workers.**
  - Worktree scan (10 s).
  - PR poll (60 s, one GraphQL request).
  - Ports (5 s).
  - Cleanup (10 min and on merge).
  - Workspace facts (30 s).
  - Title strips (250 ms).
  - Turn snapshots, one for each prompt.
  - Title resolution, one for each new session.

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

Do not let clean layers add latency. Apply these rules:

- **One state owner.** One goroutine event loop in the daemon owns the in-memory state. The daemon writes SQLite in the background after each change. Reads never go to disk.
- **Push, do not poll, for agent state.** Hooks call `agentws hook`. It writes one message to the socket and exits in 50 ms or less. It never blocks the agent. If the daemon is down, the hook drops the event and logs it to `hook.log`. `scripts/bench-hook.sh` checks the wall-time budget in CI. See [docs/adr/0006-hook-ingestion.md](docs/adr/0006-hook-ingestion.md).
- **TUI renders from a snapshot.** The daemon pushes state diffs. The TUI never runs git, gh or tmux on the render path.
- **Put heavy work in workers:** `du`, cleanup, diffs and PR polling run in a bounded pool with debounce. The diff cache uses the tree hash in its key.
- **Batch git:** one `git status --porcelain=v2 -z` for each worktree for each burst of changes. fsnotify starts it with a 300 ms debounce.
- **Poll GitHub politely:** each poll is one GraphQL request for all repos. The base interval is 60 s. It is faster only while checks run. After a failure, it uses exponential backoff. GraphQL POSTs cannot use ETags (ADR 0024).

**Budgets** (where possible, CI benchmarks enforce them):

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
