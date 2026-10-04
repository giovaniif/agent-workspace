# ADR 0046: Remote app

Status: proposed, 2026-10-03.

## Context

Sessions run in the `agentws` tmux server and are driven from the TUI. Away from the terminal there is no way to see which session needs you, answer it, or start a new one. The remote app is a phone client for that: a session list, a chat view over each session, and new sessions. Review, nvim, shells, ports, disk and cleanup stay terminal-only.

It is a feature for anyone running `agentws`, not one setup. How the phone reaches the machine (Tailscale, a LAN, a VPN, a tunnel) is the user's choice; `agentws` only exposes an HTTPS endpoint and pairs devices with it.

## Decision

### Sessions stay TUI sessions

- A session the phone talks to is the same tmux pane the terminal shows. The chat view reads the harness transcript; input goes into the pane. Nothing runs headless, so a session can move between the terminal and the phone at any time.
- `Session` gains `Transcript`, the latest `transcript_path` from the harness's hooks (Claude and Codex both send it; the daemon already reads it for usage).
- `domain.Message` is the harness-neutral chat entry: `ID`, `Cursor`, `Turn`, `Role` (`user`, `assistant`, `tool`, `system`), `Text`, an optional `Tool` (`Name`, `Summary`, `Status`) and `At`. The Claude adapter parses its JSONL transcript and the Codex adapter its rollout file into `[]Message`. Both skip unknown entries, so a harness update degrades to missing lines, not errors. Fixtures in `testdata/` pin each format.
- `Message.Cursor` is the byte offset just past the transcript line the message came from. Paging back reads earlier lines; live updates read appended bytes.

### New daemon methods

- `transcript.page` (`{"session","before","limit"}` → `{"messages","before"}`) reads a page on the connection goroutine. An empty `before` asks for the newest page; the response's `before` is the start of its oldest message, to pass back for the page before it.
- `transcript.watch` (`{"session","after"}`), with `after` the `Cursor` of the newest message the client holds, streams the messages after it like `subscribe` streams diffs. A worker per watched session tails the file with fsnotify and stops when the last watcher leaves. Nothing reads transcripts while no client watches, and the event loop never reads them.
- `session.send` (`{"session","text"}` → `{"queued"}`) uses a text queue of its own, separate from `review.send`'s review-draft queue but dispatched by the same rule: one bracketed paste and Enter once the session is idle, done or waiting. A send to a busy session waits, and the app shows it as queued.
- `session.interrupt` (`{"session"}`) sends Escape to the pane.
- `session.prompt` (`{"session"}` → `{"text","choices"}`) and `session.answer` (`{"session","choice"}`) handle a permission dialog: the daemon captures the pane, the harness adapter turns the dialog into choices and maps a choice to its keys. This is tied to each harness's dialog layout and is tested against captured fixtures; a structured path through a blocking permission hook is left to a later ADR, because it cannot meet the `agentws hook` budget as it stands.
- `session.new` gains an optional `prompt`, sent through `session.send` once the session is up.

### `agentws serve`

- A new subcommand and a new package, `internal/serve`. Like `tui`, it talks to the daemon only through `rpc` and may import `domain` types; depguard gets a `serve` rule. It starts the daemon the same way the TUI does when none is running.
- It listens on `--addr` (default `127.0.0.1:7420`). With `--cert` and `--key` it serves TLS itself (for example the files `tailscale cert` writes); without them it listens on loopback only, behind a reverse proxy on the same host. A device token never crosses a network in clear: plain HTTP on a non-loopback address is refused, with no override. A proxy that cannot reach loopback (one in a container) connects over TLS; `--self-signed` makes a certificate for that hop, which the proxy is set to trust.
- `agentws setup serve [--remove]` installs it as a systemd user unit on Linux or a launchd agent on macOS, with the PATH of the shell that ran it, like `setup bridge`.
- The HTTP API lives under `/api/v1` and is an allowlist, not a passthrough of the socket protocol:

  | Endpoint | Daemon method |
  |---|---|
  | `GET /api/v1/hello` | `status`; returns the API version and build, without auth |
  | `POST /api/v1/pair` | pairing, below |
  | `GET /api/v1/workspaces` | `workspace.list` |
  | `POST /api/v1/sessions` | `session.new` |
  | `GET /api/v1/sessions/{id}/messages?before=&limit=` | `transcript.page` |
  | `POST /api/v1/sessions/{id}/messages` | `session.send` |
  | `POST /api/v1/sessions/{id}/interrupt` | `session.interrupt` |
  | `GET /api/v1/sessions/{id}/prompt` | `session.prompt` |
  | `POST /api/v1/sessions/{id}/answer` | `session.answer` |
  | `POST /api/v1/sessions/{id}/end`, `/resume`, `/mute`, `/rename` | the matching `session.*` |
  | `GET /api/v1/stream` (WebSocket) | `subscribe`, `transcript.watch` |

- The stream sends a filtered `State` (workspaces, tasks, sessions, the launcher queue) and its diffs. The client asks for a session's messages with `{"watch":id,"after":cursor}` and drops them with `{"unwatch":id}`.
- The API is versioned by path. Adding a field or an endpoint keeps `v1`; changing a meaning makes `v2`, and `serve` keeps answering `v1` until a release notes its removal. `internal/serve/testdata/` holds golden responses.

### Pairing and devices

- `agentws remote pair [--name phone]` asks the daemon for a pairing code and prints a QR code and the plain URL, `https://<public url>/#pair=<code>`. The public URL comes from `--url` or `[serve] url` in `config.toml`.
- A code is 8 characters from an alphabet without look-alikes, valid for 5 minutes and single use. A wrong code voids nothing, so nobody can cancel a pairing by guessing; instead `pair` accepts at most 5 failed tries a minute from one address and 20 a minute in all, which keeps guessing a 40-bit code in 5 minutes out of reach. `POST /api/v1/pair` swaps it for a device token: 32 random bytes, shown once, stored only as its SHA-256 in a `devices` table with a name, created and last-seen times.
- Every other request carries `Authorization: Bearer <token>`. A WebSocket sends the token in its first frame, since browsers cannot set headers on one, and is closed if that frame is late or wrong. The stream also checks `Origin` against the public URL.
- `agentws remote devices` lists devices; `agentws remote revoke <id>` deletes one and closes its open streams.
- Code expiry, attempt counting, token checks and revocation are pure rules in `domain`, table-tested.

### The app is a PWA served by `serve`

- The source is in `web/`: React, TypeScript and Vite. `make web` builds it into `internal/serve/dist`, which `serve` embeds with `go:embed`. The binary needs no Node at runtime; building the UI does. A committed placeholder `dist/index.html` keeps `go build` working without Node and tells the user to run `make web`. Releases always build the UI.
- Screens:
  - **Sessions:** grouped by attention (needs you, working, done and unread, idle), with name, `repo@branch`, harness, model and context left.
  - **Session:** the transcript with tool calls collapsed, a composer, interrupt, a permission card with the choices when one is pending, and a header with state, model and limits.
  - **New session:** workspace, work item or free-form prompt, harness, model and effort.
  - **Settings:** server, this device, sign out.
- One server per installed app. A PWA belongs to its origin, so each server serves its own app and the user installs one icon per server. There is no server list inside the app.
- Pairing happens inside the installed app. On iOS a Home Screen app does not share storage with Safari, so a token saved in Safari would be lost: opening the pair URL in a browser shows "Add to Home Screen", and the installed app then asks for the code (prefilled when the URL carries it).
- The service worker caches the app shell keyed by the build. When `hello` reports another build, the app reloads, so a cached UI never talks to a newer API it does not know.
- `scripts/lint-comments` extends to `web/src`.

### Push (later phase)

- Web Push with VAPID: `serve` creates a key pair in `$AGENTWS_HOME/vapid` (600) on first use. After pairing the app may subscribe; the subscription is stored with its device and dropped on revoke or when the push service answers 404 or 410.
- A `PushProvider` port in `app`, fed from the same banners `notify.stream` carries (ADR 0016), sends title, body and the session URL. iOS shows a notification for every push, so `serve` never sends silent ones.
- It works on iOS 16.4+ only for an app added to the Home Screen, with no Apple developer account.

## Why

- **Read the transcript and type into the pane** keeps one session usable from both places and reuses hooks, the state machine and the queued-send path. Running the harness headless would give cleaner events but split sessions into two kinds.
- **A separate `serve` process** keeps HTTP, TLS and auth out of the daemon and keeps its budgets. It reaches the daemon the way the TUI does, so the layers do not change.
- **An allowlisted API** keeps the remote surface small: no shell, nvim, ports, disk, cleanup or review endpoints.
- **A PWA** ships inside the one binary, installs without an app store, and gets native notifications through Web Push with no Apple account or relay.
- **Pairing by short-lived code** works through any network path the user chooses and needs no accounts.

## Rejected

- **A native Swift app:** without a paid Apple account it cannot receive push and must be re-signed every 7 days; with one, every self-hoster needs their own APNs key or a project-run relay.
- **Headless sessions** (`claude -p --input-format stream-json`, the Agent SDK, `codex exec`): no terminal pane, so the session could not be picked up in the TUI.
- **Claude Code's own remote control:** Claude only, and it knows nothing about workspaces, worktrees, attention or the launcher.
- **HTTP inside the daemon:** puts TLS, auth and slow clients next to the event loop.
- **A server list in one app:** a push subscription is bound to one origin's service worker and one VAPID key, and a cross-origin app would need CORS on every server.
- **Preact:** a smaller runtime, but the app is cached by its service worker and embedded in the binary, so the size rarely matters. React's libraries (transcript virtualization, markdown, testing) work without a compat layer.
- **Self-signed certificates with pinning:** iOS will not run a service worker or push on an origin it does not trust without installing a profile.

## Consequences

- The phone must reach `serve` over HTTPS with a certificate the phone trusts. Setup docs cover Tailscale (`tailscale cert` or a reverse proxy), a LAN or VPN behind a reverse proxy with Let's Encrypt, and a Cloudflare Tunnel with Access in front.
- `serve` is remote control of the machine's agents. Device tokens are the only gate besides the network path, so the docs say not to expose it to the internet without a tunnel or proxy that adds its own authentication.
- Permission answers depend on each harness's dialog layout until the hook-based path exists; an adapter test fails when a captured dialog no longer parses.
- Transcript formats are not public APIs. A harness update can drop messages from the chat view until the parser catches up; the pane itself is unaffected.
- `web/` brings Node into the build. Web tests have parity with Go tests in the `tdd` job (ADR 0048).

## Phases

1. `transcript.page`, `Session.Transcript` and the Claude and Codex parsers.
2. `agentws serve`, the allowlisted API, pairing, devices and `setup serve`.
3. The PWA shell: pairing, the session list, the read-only transcript.
4. Chat: `session.send`, `session.interrupt`, `transcript.watch`, live updates.
5. New sessions from the app, and permission answers.
6. Web Push.
