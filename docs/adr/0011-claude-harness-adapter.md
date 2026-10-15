# ADR 0011: Claude Code harness adapter

Status: accepted, 2026-09-29.

## Decision

- `crew setup claude` merges into `$CLAUDE_CONFIG_DIR/settings.json` (default `~/.claude/settings.json`). For each of `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Notification`, `PermissionRequest`, `Stop`, `SubagentStop` and `SessionEnd` it appends one matcher group running `<crew> hook --harness claude --event <name>`. The user's groups stay first and untouched.
- Key order is kept, and the file is written with a two-space indent. Running setup again strips every crew entry, whatever binary path it has, and adds them back, so the bytes match.
- Before the first write, the original file is copied to `settings.json.crew-backup`, unless it already holds crew entries. `--remove` strips crew entries. If what is left equals the backup as JSON, the backup's bytes are written back. Otherwise the stripped file is kept, so edits made after setup survive. The backup is then deleted.
- The status line becomes `<crew> statusline --harness claude [--chain '<user command>']`. Other `statusLine` fields such as `padding` stay. The user's command is kept inside ours as one shell-quoted argument, so remove can restore it without a sidecar file. The wrapper runs it with `sh -c` on the same stdin and copies its stdout through unchanged. At the same time it sends one `statusline` request with a 50 ms deadline. It always exits 0.
- The wrapper reads `model.id` (shortened to `opus-5.5`; `model.display_name` when there is no id; #88), `effort.level`, `context_window.remaining_percentage` and every key under `rate_limits`. `domain.Session.Report` keeps any value the status line does not know yet, such as context before the first turn. The windows go in `Session.Limits`, and `Usage.LimitUsedPercent` is the fullest of them. `Usage` itself stays comparable.
- Mapping: `Notification` uses its `notification_type`. `permission_prompt` maps to `permission_request`, `auth_success` is ignored, and anything else maps to `waiting_for_input`. `SubagentStart` and `SubagentStop` map to `subagent_start` and `subagent_stop` (ADR 0019), which the state machine treats like tool events: progress inside the parent's turn, never its end. The name tables and those rules are in `domain`. `adapters/claude.Event` only decodes the payload field.
- `app.HarnessAdapter` is the launch port: `Harness()` and `Launch(LaunchRequest) PaneSpec`. Claude runs `claude [--model m] [--effort e] [-- prompt]` in the session's dir. The daemon's `session.launch` creates the pane through `TerminalHost` on the connection goroutine, then adds an `idle` session on that pane.

## Why

- The user's settings file is theirs. Appending instead of rewriting keeps hooks such as tmux integrations firing. Restoring the backup's bytes only when nothing else changed means remove never throws away a later edit.
- Claude pipes the same JSON to the status line on every render, and nothing else exposes rate limits. Chaining keeps the user's status line as it was.

## Limits

- Claude 2.1.x sends only `five_hour` and `seven_day` in `rate_limits`. `/usage` also shows a per-model weekly window, but the status line does not carry it. Any extra key under `rate_limits` is stored as its own window, so per-model windows appear once Claude sends them.
