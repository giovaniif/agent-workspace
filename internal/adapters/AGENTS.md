# internal/adapters

Implementations of the `app` ports. All exec, disk and network calls are here. git, tmux, setup recipes and onboarding have their own notes: [git/](git/AGENTS.md), [tmux/](tmux/AGENTS.md), [setup/](setup/AGENTS.md), [onboard/](onboard/AGENTS.md).

## Harness adapters

`app.HarnessAdapter` changes an `app.LaunchRequest` into the `PaneSpec` that runs the harness.

- **Claude** (`claude`): `agentws setup claude [--remove]` merges agentws hooks and the status-line wrapper into `$CLAUDE_CONFIG_DIR/settings.json` (default `~/.claude`). It backs up the file, and `--remove` undoes the merge. `agentws statusline` runs the user's own status line too, and reports model, effort, context left and rate limits. Tests set `CLAUDE_CONFIG_DIR` to a temp dir and never touch the real `~/.claude`. See [ADR 0011](../../docs/adr/0011-claude-harness-adapter.md).
- **Codex** (`codex`):
  - the `agentws setup codex [--remove]` merge into `$CODEX_HOME/hooks.json`
  - hook and notify payload parsing
  - the rollout reader that supplies model, effort, context and limits
  - pane lookup and launching. `codex.Adapter` is registered beside the Claude adapter, so `session.launch --harness codex` works.

  Its tests, and all other tests that touch Codex config, use a temp `CODEX_HOME`, never the real `~/.codex`. The mapping, the formulas and the source of each number are in [codex/README.md](codex/README.md) and [ADR 0010](../../docs/adr/0010-codex-adapter.md).
- **omp** (`omp`): `agentws setup omp [--remove]` writes one file, `$PI_CODING_AGENT_DIR/hooks/post/agentws.ts` (default `~/.omp/agent`). Its first line is the ownership marker. If a file does not have the marker, the command reports it and never overwrites or removes it. The hook names come from the domain harness table. The hook sends `model` and `effort` in its JSON, and the daemon reports them as status. Launch builds `omp [--resume id] [--model m] [--thinking e] -- <prompt>`. The TUI draws from `$AGENTWS_HOME/cache/omp-models.json` immediately. Then a command runs `omp models --json` and replaces the cache and the open list. With no catalog, the model stays a text field. omp has no limits row and no session-file reader. Tests use a temp `PI_CODING_AGENT_DIR`. If `bun` is installed, `go test ./internal/adapters/omp` also runs the hook file under `bun`.
- **Transcripts.** `claude.TranscriptParser` reads the JSONL transcript of Claude into `[]domain.Message`. `codex.TranscriptParser` reads the rollout file into `[]domain.Message`.
  - `Parse(data, base)` takes bytes that start at a line boundary at file offset `base`. It returns the messages of the complete lines and the offset just after the last one. It keeps a partial last line for the next call.
  - The zero value is ready. To tail a file, keep one parser for each file. Thus a tool result in a later chunk comes back as the full tool message (same `ID`, the `Cursor` of the call).
  - A fresh parser can find a result whose call it never saw. It then returns a tool message with only `ID`, `Status`, `Text` and `Cursor`.
  - `ParseTranscript(data, base)` is a one-shot parse. The parsers skip unknown lines, entry types and blocks.
  - `Cursor` is the offset just after the source line of a message. Claude IDs are entry `uuid`s and `tool_use` ids, and `Turn` is the `promptId`. Codex IDs are `call_id`s for tools and the start offset of the line for all other messages. `Turn` is the ID of the user message of the turn.
  - Fixtures are in `testdata/transcript/`. For the goldens, run `go test ./internal/adapters/claude/ ./internal/adapters/codex/ -run Transcript -update`, then review the diff. The Codex fixture was recorded from `codex exec` 0.93 against a scripted local model, then scrubbed.
- **Permission prompts.** `claude.Adapter` and `codex.Adapter` implement `app.PermissionPrompter`. `PermissionPrompt(screen)` changes a captured pane into the dialog text and numbered choices, or reports that there is no dialog. Each choice has the keys that answer it. For Claude, this is the digit. For Codex, it is the shown shortcut, for example `y`, `p`, `a` or `Escape`.
  - The shared scan is `internal/adapters/dialog`. It needs these items:
    - the last numbered block `1.`..`n.`, with exactly one cursor marker
    - a maximum of 4 non-empty lines after the block
    - a known question line above the block.

    Thus the scan does not recognise old scrollback and unknown dialogs.
  - Fixtures are in the `testdata/prompt/` of each adapter. They are not real captures: they are built from the dialog strings in the installed binaries (Claude 2.1.288, Codex 0.93). When a dialog changes, capture them again with `tmux capture-pane -p`. Codex 0.93 has no web-fetch approval dialog.
- `claude.Installed` and `codex.Installed` run the merge of Setup in memory. They report if the merge changes nothing. Thus the walkthrough and the CLI agree.

## Others

- **notify**:
  - If `terminal-notifier` is on `PATH` at daemon start, it uses `terminal-notifier`. Banners are grouped for each session, and a click runs `agentws focus <id>`. The banner of a session goes away when the session resumes or gets focus.
  - Else it uses `osascript`.
  - With both, `Fallback` posts through `osascript` when `terminal-notifier` fails. After install, macOS often has the notifications of `terminal-notifier` off.
  - With neither, it uses `Silent`.

  `Relay` posts the banners of a remote daemon for `agentws notify bridge`. Both implement `app.Notifier`. `osascript` also implements `app.Foreground`. See [ADR 0016](../../docs/adr/0016-notifications-and-attention.md).
- **webpush** (`app.PushProvider`): `github.com/SherClockHolmes/webpush-go`, aes128gcm payloads, VAPID JWT with `sub` the project URL, TTL 24 h, urgency high. The key pair is JSON in the file that the adapter gets (`$AGENTWS_HOME/vapid`, 600). The adapter creates it on first use. 404 and 410 wrap `app.ErrPushGone`. The client never follows redirects. After resolution, the default client refuses to connect to loopback, private, link-local, multicast, unspecified and `100.64.0.0/10` addresses. Thus a paired device cannot point the daemon at an internal service. Tests run a TLS `httptest` push service that decrypts the payload and checks the VAPID signature: `go test ./internal/adapters/webpush/`.
- **launchd**: writes and loads (or unloads and deletes) the launchd agent for `agentws setup bridge` and `agentws setup serve`. Tests use a temp dir and a fake runner.
- **systemd**: on Linux, writes and starts (or stops and deletes) the systemd user unit for `agentws setup serve`. It has the same idempotency and `.bak` backup as launchd. Tests use a temp dir and a fake runner, never `~/.config/systemd`.
  - If `XDG_RUNTIME_DIR` and `DBUS_SESSION_BUS_ADDRESS` are not set, `systemd.Exec` sets them from `/run/user/<uid>`. Non-interactive ssh has neither.
  - If that dir is missing, it still runs systemctl, but adds a `loginctl enable-linger` hint to its error.
- **procs** (`app.ProcessTable`): `netstat -anv -p tcp` for listening sockets, then one `lsof -a -d cwd -p <pids>` for their group, command and cwd. A refresh (both commands) must take less than 50 ms. `BenchmarkPortsRefresh` fails above that (measured 19 ms). It also runs one `lsof -d cwd` for the holders check of cleanup.
- **fs**:
  - `WorkspaceFS` (stat and readdir only, no git).
  - `Du` (`du -sk -P`; `go test -tags integration -run Disk ./internal/adapters/fs/`).
  - The trash and `AuditLog`.
  - `Transcripts` (`app.TranscriptFiles` and `app.TranscriptWatcher`): size and ranged reads of a transcript, and an fsnotify watch of the file. Until the file exists, it watches the directory of the file. Test: `go test ./internal/adapters/fs/ -run Transcript`.
- **github**: one read-only `gh api graphql` request for all repos in each poll. Tests use a fake `gh`, and nothing gets to GitHub. GraphQL POSTs cannot use ETags ([ADR 0024](../../docs/adr/0024-pr-board.md)). It also gets `gh pr view` titles for naming.
- **linear**: issue titles through the Linear API, tested against a mocked API.
- **sqlite**: `modernc.org/sqlite` (no cgo), embedded migrations, write-behind. The file is `$AGENTWS_HOME/state.db`. See [ADR 0004](../../docs/adr/0004-sqlite-store.md).
- **loginshell**: `Path` runs `$SHELL -l -c` (else `/bin/sh`) and reads its PATH after a marker. Thus it ignores profile output. Tests use a temp `HOME` with its own `.profile`: `go test ./internal/adapters/loginshell/`.
- **nvim**: controls the nvim of each session through `nvim --listen`. `Installed` is `exec.LookPath`.
