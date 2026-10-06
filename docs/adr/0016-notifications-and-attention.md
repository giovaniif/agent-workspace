# ADR 0016: Notifications and attention

Status: accepted, 2026-09-30.

## Decision

- `Session.Apply` already returns `EffectNotify`. The daemon now performs it. On the loop, `domain.BannerFor` turns the effect into a `Banner` (title is the session name from `NameFor`, falling back to the harness; body is `needs permission`, `waiting` or `done`). Muted sessions give no banner. `domain.Coalescer` lets one banner per session through each 10 s.
- The banner goes to a bounded queue read by one worker. The worker calls the `app.Notifier` port. If the session is focused, it first asks the `app.Foreground` port whether a terminal app is in front, and drops the banner if so. Both ports run `osascript`, so neither runs on the loop. A full queue drops the banner.
- `adapters/notify.Osascript` implements both ports. Title, body and sound reach AppleScript as argv (`on run argv`), never spliced into the script. The frontmost check reads the process name from System Events and matches it, lowercased, against a list of terminal apps. A failed check counts as not in front, so a banner is never lost on a guess.
- Sound per event is optional: `$AGENTWS_HOME/notify.json`, `{"sounds":{"permission":"Glass","done":"Hero"}}`, read at daemon start. A macOS sound name, per `permission`, `waiting` or `done`.
- `m` in the TUI toggles `Session.Muted` through `session.mute`. Mute is stored with the session and silences banners only. The unread marker still appears, since `Apply` sets it independently.
- `Session.Focused` is now set. `enter` in the TUI calls `session.focus`, which focuses that session, blurs the one that had focus, and clears its unread marker. Focus is cleared for every session when the daemon starts.
- Claude and Codex go through the same path: their hooks become harness events, `Apply` yields the effects, and one code path turns them into banners. A test runs the same fixture for both.

## Why

- The state machine already decided when to notify. Keeping mute, coalescing and wording in `domain` keeps them table-tested, and leaves the daemon to wire ports.
- Coalescing sits on the loop, not in the worker, so a burst never fills the queue.

## Limits

- Focus is sticky. A session stays focused until another one is focused with `enter`, so leaving the TUI on session A with the terminal in front hides A's banners. Moving the selection alone does not focus.
- A coalesced banner is lost, not delayed. A `done` right after a `permission` on the same session, within 10 s, shows no second banner. The sidebar still shows the state.
- Codex has no waiting event: its hooks map to permission and done only.
- The frontmost check needs macOS Automation access for System Events. Without it the check fails and banners show even when the terminal is in front.

## Amendment, 2026-10-01 (#131): banner content

- `domain.BannerFor(BannerInput)` builds the banner from what the loop already holds: the session, its name, its worktrees and its recent `SessionEvent`s (the last 20, already kept for the session card), plus the time. Nothing new is read from disk or exec'd.
- Title: the name (or harness), then ` · repo@branch` from the first worktree (repo as its last path element, branch cut to 29 runes), `+N` for more worktrees; 80 runes at most.
- Body, 120 runes at most, from the current turn (events since the last prompt): permission gives `needs permission: <tool>: <target>` or the hook's message; waiting gives `asks: <question>` (the last paragraph of the last message, when it ends in `?`) or `waiting: <message>`; done gives the first line of the last assistant message and the time since the prompt, `usage limit: …` or `error: …` when that line reports a limit or an API error, or `done in 4m12s` with no message. With nothing known it falls back to the bare state word.
- Secrets are masked before the cut: `token=…`, `password: …`, `API_KEY=…` and well-known token prefixes (`sk-`, `ghp_`, `github_pat_`, `xoxb-`, `AKIA`). Only the first line of a message is shown, never a prompt.
- `internal/domain/testdata/banners.golden` pins a set of realistic banners.
- Tests never post a real banner: the e2e suite puts a fake `osascript` first on `PATH` that logs its argv to `$AGENTWS_E2E/osascript.log`, and `session_states.txtar` asserts banners reached it. Only the e2e suite runs `daemon.Run`; every other test injects a fake notifier.

Limits of the amendment:

- "N files changed" is not shown: the loop holds no per-turn file count (turn snapshots live in git). Done shows the message line and elapsed time instead.
- `osascript`'s `display notification` cannot group banners or open the session on click. That needs another notifier (such as `terminal-notifier`), left for a later issue.
- There is no error or limit agent state; those are recognised from the last message text only.

## Amendment, 2026-10-01 (#134): terminal-notifier

- At daemon start, `notify.Detect` picks `adapters/notify.TerminalNotifier` when `terminal-notifier` is on `PATH`, else `Osascript` as before. The choice is made once; banners never pay for a lookup. The frontmost check stays on `osascript` with either backend.
- `Banner` carries `Group` (the session id) and `Terminal` (a bundle id). terminal-notifier gets `-title`, `-message`, `-group <session id>`, `-sound` when one is set in `notify.json`, `-activate <bundle id>` when known, and `-execute` running `AGENTWS_HOME=<home> <agentws> focus <session id>`, every part shell-quoted. One banner per group is kept, so a session's newer banner replaces its older one. A message starting with `[` or `-` gets a leading backslash so terminal-notifier does not read it as an option. Mute and coalescing are unchanged: they decide before any backend runs.
- `agentws focus <id>` calls `session.focus` over rpc, the same call as `enter` in the TUI: the daemon shows the session's pane in the main slot and selects it (only the daemon drives tmux), focuses it and clears its unread marker.
- `app.Notifier` gains `Remove(group)`. The loop remembers which sessions have a banner up and queues a removal (`-remove <session id>`) when such a session is focused or leaves permission, waiting or done for running or idle (`domain.BannerStale`). The osascript backend's `Remove` does nothing.
- The terminal to activate comes from the client: `agentws` (attach) sends `domain.TerminalBundle(__CFBundleIdentifier, TERM_PROGRAM)` in `client.open`, and the daemon keeps the last one. `__CFBundleIdentifier` is set by macOS for processes started from an app; `TERM_PROGRAM` is mapped for Terminal, iTerm2, Ghostty, WezTerm, VS Code and Warp.
- The e2e suite has a fake `terminal-notifier` next to the fake `osascript`, logging its argv to `$AGENTWS_E2E/terminal-notifier.log`; `session_states.txtar` checks the grouped banner, runs `agentws focus` and checks the removal.

Limits of the amendment:

- The terminal is the one `agentws` last attached from. A client attached from another terminal later, or no attach since the daemon started, means the wrong app or none is activated; the pane is still selected.
- Inside another multiplexer or over ssh, `__CFBundleIdentifier` and `TERM_PROGRAM` may name the wrong app or nothing.
- The click command runs with terminal-notifier's environment, so it names the `agentws` binary and `AGENTWS_HOME` the daemon had at start. Replacing the binary in place keeps working; moving it does not until the daemon restarts.
- A banner removed while macOS has it on screen may stay until it times out; removal clears it from Notification Center.
- A failed `-remove` is logged, not retried; the stale banner stays until dismissed or replaced by the session's next banner (same group).

## Amendment, 2026-10-01: notification bridge for a remote daemon

- A daemon on a remote host (a VPS reached over ssh) cannot post banners on the user's Mac. `notify.stream` is an rpc stream: an empty Result, then one `rpc.Notice` per banner the loop lets through (after mute and coalescing, before the worker's frontmost check, with `Focused` set) and per withdrawal (`Remove`).
- `agentws notify stream` prints those notices as JSON lines. `agentws notify bridge [--remote-bin path] <ssh host>` runs it over `ssh -T` on the Mac, posts each banner through the local backend (`notify.Relay`), drops a focused session's banner while a terminal is in front, and reconnects with a backoff of 2 s doubling to 1 min. A click runs `ssh -T <host> <remote-bin> focus <session id>` (`TerminalNotifier.FocusCmd`).
- `notify.Select` returns `Silent` when neither `terminal-notifier` nor `osascript` is on `PATH`, so a Linux daemon logs nothing per banner. With both, it returns `Fallback`: a banner terminal-notifier fails to post (macOS often has its notifications off after install) goes through osascript, without grouping or click, and the error, with the tool's stderr line, is logged.

- `agentws setup bridge [--remote-bin path] [--remove] <ssh host>` keeps the bridge running: `adapters/launchd` writes `~/Library/LaunchAgents/dev.agentws.bridge.<host>.plist` (a host with characters other than letters, digits, `.` and `-` has them replaced by `-` and gets 8 hex digits of its SHA-256, so `me@vps` and `me-vps` stay apart) (`RunAtLoad`, `KeepAlive`, the `PATH` and `AGENTWS_HOME` of the shell that ran it, output to `$AGENTWS_HOME/bridge-<host>.log`) and loads it with `launchctl bootout` then `bootstrap gui/<uid>`. The same arguments again change nothing, but load the agent if `launchctl print` finds it unloaded; different ones back the old file up as `.bak`, rewrite and reload it. `--remove` boots it out if loaded and deletes the file; a failed bootout keeps the file, and an unreadable directory stops before `launchctl`. Tests use a temp dir and a fake runner.

Limits:

- The agent runs the binary that ran setup, by path; moving or deleting it breaks the agent until setup runs again.
- The bridge needs key-based ssh with no prompt, and `agentws` on the remote's non-interactive `PATH` (else `--remote-bin`).
- Banners posted while the bridge is disconnected are lost; the sidebar still shows the state.

## Amendment, 2026-10-06 (#206): presence for Web Push

- The terminal-in-front check above reads the Mac's screen, which a daemon on a headless host reached over ssh cannot see. Web Push (ADR 0046) gets its own presence rule instead: the owner is at the terminal while someone typed in a client attached to the `agentws` tmux server less than `[push] away_after` ago (default 2 minutes). While present, pushes are held, not dropped, and one is sent per session when the owner goes away with that session still needing them. Details are in ADR 0046's amendment of the same date.
- The Mac banner path is unchanged: it keeps the frontmost check, and it does not use tmux presence.
