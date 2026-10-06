# ADR 0049: Native macOS app

Status: proposed, 2026-10-06. The screens and interactions are in [docs/macos-app-design.md](../macos-app-design.md).

## Context

FEATURES.md lists a native macOS client as P2, "on the same daemon". The owner wants it now: everything the TUI does, in a Mac window, for a daemon on the same Mac or on a server reached over SSH. The owner's own setup is the second case: a headless Linux box with the daemon and repos, and a Mac that only views it.

What exists:

- The socket protocol (ADR 0005): newline-delimited JSON with request/response calls and a `subscribe` stream of raw domain structs. Derived values (names, banners, sidebar order, quotas) are computed by each client in Go from `domain`.
- `agentws serve` (ADR 0046) exposes an allowlist of it over HTTPS for the phone, with derived values precomputed in `internal/serve/view.go`. It has no review, shell, nvim, disk, ports or terminal endpoints, by design.
- `agentws notify bridge` (ADR 0016) already runs `ssh -T host agentws notify stream` from a Mac and posts the daemon's banners there.
- The tmux adapter (ADR 0003) parks each agent pane in its own window of the `agentws` server and swaps it into the TUI client's main slot.

ADR 0046 rejected a native phone app because of APNs and 7-day re-signing. Neither applies on macOS: a running Mac app posts local notifications without APNs, and a Developer ID build does not expire.

## Decision

### Where the code lives

- In this repo under `macos/`: a Swift package `AgentwsKit` (protocol types, transport, state, view model; no UI) tested with `swift test`, and an Xcode app target `agentws.app` that depends on it. SwiftUI, with AppKit where SwiftUI falls short (the terminal view, the split view's column behaviour, the menu bar extra's menu). Minimum macOS 15.
- The app never re-implements a `domain` rule. Anything derived (session name, `where`, banner text, state since, sidebar order, quotas, the PR board, cleanup plans) arrives precomputed from Go, as it does for the phone.

### One transport: `agentws rpc` over stdio

- New subcommand `agentws rpc`: a stdio bridge to the daemon's socket. It reads protocol lines on stdin and writes them on stdout, so the app speaks the existing protocol, build handshake included (ADR 0039).
- Locally the app runs its bundled `agentws rpc`. Remotely it runs `ssh -T -o BatchMode=yes -o ServerAliveInterval=15 <host> <remote-bin> rpc`. Same code path, same tests; SSH does auth and encryption, so there is no pairing, token or TLS for the Mac app.
- The bridge never starts a daemon. A daemon spawned under a bare `ssh -T` would give every agent pane a login-less PATH (the reason `notify stream` waits instead). When nothing listens it answers `unavailable`, and the app offers the setup below.
- Like `serve`, the bridge is its own process that reaches the daemon only through `rpc`, and may import `domain`. It is not an allowlist: the app is the same user on the same machine as the TUI, so it gets every method.

### A derived view, shared with serve

- Move the view in `internal/serve/view.go` into a package both use (`internal/view`, importing only `rpc` and `domain`). `serve` keeps its JSON exactly; its goldens prove it.
- `agentws rpc` answers one method of its own, `view.subscribe`: the derived state and diffs, a superset of the phone's stream. Sessions also carry `order`: `domain.Sidebar`'s rule over one flat list, since the app shows no task headers (sessions that need you first, then the rest in the daemon's order), `board` (the PR board of ADR 0024) and their worktrees' ports. Events, subagents and review drafts are included, since the Mac app shows them.
- Every other method passes through to the daemon unchanged.

### Review highlighting in Go

- `review.open` returns plain diff lines; the TUI highlights them in-process with the curated chroma lexers in `internal/tui/syntax`. The bridge adds a `tokens` option to `review.open`: each line comes with `[start, end, class]` spans from the same lexers, and the app maps classes to its theme. The lexers move to a package the bridge and the TUI share.
- So both clients highlight the same way, and the app needs no tree-sitter grammars.

### Terminals: tmux control mode

- New daemon method `client.native` (`{"cols","rows"}` → `{"argv"}`), like `client.open`: it prepares a dedicated tmux session on the `agentws` server that links every parked agent and shell window, and returns the argv of a control-mode client for it (`tmux -L <socket> -C attach -t <session>`). The daemon still owns tmux; only `internal/adapters/tmux` builds the command.
- The app runs that argv locally, or over the same SSH host (`ssh -T host -- <argv>`, one connection per window, multiplexed when the user's config has `ControlMaster`). It parses control-mode notifications (`%output`, `%window-add`, `%unlinked-window-close`, `%exit`), feeds each pane's bytes into a SwiftTerm terminal view keyed by pane ID, and sends keys with `send-keys -H -t <pane>`. A view that appears first draws `capture-pane -p -e -J -S -2000` for its history.
- Sizes: the app sets each window's size with `refresh-client -C @<window>:<cols>x<rows>`. A pane the TUI has swapped into its own main slot has the TUI's size; the app draws it at that size, letterboxed, and says the TUI is showing it.
- **Spike first.** One issue builds a throwaway control-mode client against a real `agentws` server, locally and over SSH, before any app code depends on it. It must show: output of 4 busy panes at once with input-to-echo under 30 ms locally and under one round trip plus 10 ms over SSH, correct colours and wide characters, mouse reporting to Claude Code and nvim, and resize without corrupting the screen. If it fails, the fallback is one PTY per visible pane running `tmux attach` to a grouped session on that pane's window, through `ssh -tt` when remote; this ADR then gets an amendment.

### Attention

- Banners: the app subscribes to `notify.stream` through the bridge and posts them with `UNUserNotificationCenter`, one per session (the session ID as the thread and request identifier, so a newer one replaces it), withdrawn on the stream's withdrawal. Actions map to existing methods: Allow (`session.answer`), Reply (`session.send`), Open (select the session).
- On a Mac running the app, `agentws notify bridge` and its launchd agent are redundant. The app offers to remove them (`agentws setup bridge --remove`) so banners do not come twice.
- Presence: new daemon method `client.viewing` (`{"session","front"}`), kept per connection like `device.viewing` and dropped when the connection closes. The daemon skips the banner for the session in view while the app is in front, and the push gate (ADR 0046, #206) counts an app in front as "at the terminal".
- The permission card uses `session.prompt` and `session.answer`, including their stale-prompt check.

### Setting up a server from the app

- The app bundles `agentws` for `darwin/arm64`, `darwin/amd64`, `linux/amd64` and `linux/arm64`. goreleaser stops ignoring `linux/arm64`.
- Setup runs these over SSH, each one idempotent:
  1. `uname -sm` and `command -v agentws`, plus `agentws version` when it is found.
  2. If agentws is missing or another build: stream the matching binary to `~/.local/bin/agentws.new`, `chmod +x`, then `mv` over the old one. The server needs no internet access, and always matches the app's build.
  3. `agentws setup daemon` (new, like `setup serve`): a systemd user unit (`agentws-daemon.service`) on Linux or a launchd agent on macOS that runs `agentws daemon` with the PATH and `AGENTWS_HOME` of a login shell. It reads the login PATH the way the daemon already does for agent panes. When `loginctl show-user` says linger is off, it says so: without linger the unit, and with it the tmux server and every session, stops at logout. The app shows the one command that fixes it, `sudo loginctl enable-linger $USER`, which it cannot run itself.
  4. Tool checks and hooks through the existing walkthrough methods (`onboarding.status`, `onboarding.install`; ADR 0040).
  5. `workspace.add` for the chosen folder; the folder browser uses `workspace.dirs` (ADR 0047).
- An update of the app updates each server the same way when its build differs. The update restarts the daemon, which does not end sessions: they live in tmux.

### Distribution

- Signed with Developer ID and notarized in CI on a macOS runner, released as a DMG beside the existing archives and as a Homebrew cask. The app's version is the Go release tag, so its bundled binaries and the handshake agree.
- `agentws` on the PATH is a link into the bundle (Settings › General), so the TUI and the app are the same build.

### Tests and repo rules

- `AgentwsKit` is tested with Swift Testing. Protocol decoding is tested against the Go side's golden JSON: the bridge's `view.subscribe` output and the `serve` goldens are written to `testdata/` by Go tests and read by Swift tests, so a field renamed on one side fails the other.
- `scripts/tdd-check` treats `macos/**/Tests/**/*.swift` as tests, as ADR 0048 did for web: on base it runs `swift test --filter` for each changed test file, and a base without `macos/Package.swift` counts as failing. `scripts/lint-comments` covers Swift. CI adds a macOS job that builds the app and runs `swift test`.
- UI is checked by snapshot tests of the main views against seeded fake state, and by PR screenshots as for the TUI.

## Why

- **stdio over SSH** reuses the user's keys, agent, `~/.ssh/config`, Tailscale SSH and jump hosts, with no listener to open and no auth to build. It is the same pattern as the notify bridge, which already works.
- **One transport for local and remote** means the remote case, which is the owner's daily one, is never the second-class path.
- **Derived values from Go** keeps every rule in `domain`, table-tested once, and keeps the app, the TUI and the phone in agreement.
- **Control mode** gives one connection for all panes, native views per pane, no nested tmux status line or prefix keys, and keeps sessions in tmux so the TUI and the app see the same panes. iTerm2 has used it for years.
- **Bundling the server binary** removes the "install it first" step and guarantees the build handshake passes.

## Rejected

- **The app talks to `agentws serve`:** serve is an allowlist for a phone and lacks review, shells, nvim, disk, ports and terminals. Widening it would put the whole surface behind a network listener and device tokens.
- **A port-forwarded Unix socket (`ssh -L`):** stale socket files, no way to start anything on the far side, and a second mechanism beside the stdio one the notify bridge already uses.
- **Re-implementing domain rules in Swift:** two copies of naming, banners and quotas that drift.
- **A terminal per pane via `ssh -tt … tmux attach`** as the default: one SSH session per visible pane, nested tmux keys and status, and a resize fight between clients. Kept only as the fallback.
- **Headless sessions or a chat-only view:** ADR 0046's reasons hold; the terminal is the session.
- **tree-sitter in the app:** a second highlighter that would not match the TUI.
- **A Go GUI toolkit or Electron:** not native, and the owner asked for Swift.

## Consequences

- A Swift toolchain, Xcode and a paid Apple developer account for signed releases. Unsigned local builds still work for contributors.
- `agentws rpc`, `view.subscribe`, `client.native`, `client.viewing` and `setup daemon` are new public surface: CLI help, ADR-listed methods and goldens.
- The app is not an allowlist and runs anything the user can run on that machine, which is what SSH access already grants.
- Two clients can show the same pane at different sizes; the app letterboxes rather than resizing a pane the TUI is showing.
- Linger is a manual step on some servers.
- macOS CI minutes grow by the app build and tests.

## Phases

Tracked in the `macOS app` milestone.

1. Spike: control mode over SSH (#216).
2. `agentws rpc` (#217), `internal/view` and `view.subscribe` (#218), `setup daemon` (#219), `linux/arm64` builds (#220).
3. `macos/` scaffold and repo rules (#224); `AgentwsKit` (#225); the read-only main window (#226).
4. `client.native` (#221) and embedded terminals (#227).
5. `client.viewing` (#222) and attention: notifications, menu bar, Dock badge, permission card (#228).
6. New session sheet and launcher (#229).
7. `review.open` tokens (#223) and review mode (#230).
8. Worktrees, disk and ports (#231).
9. Settings (#232); servers, first-run setup and server install (#233).
10. Shell and nvim views (#234).
11. Signing, notarization, DMG and cask (#235).
