# ADR 0018: Model and effort switching

Status: accepted, 2026-09-30.

## Decision

- `M` and `E` in the TUI open a picker of models or effort levels for the selected session's harness (`domain.SwitchChoices`). `j`/`k`, a digit, `enter` and `esc` drive it. The choice goes to the daemon as `session.switch` (`{"session_id","kind","value"}` → `Session`).
- The rules are pure functions on `domain.Session`. `RequestSwitch` queues a switch, replacing any earlier one of the same kind. `Dispatch(now)` marks the queued ones as sent and returns them, but only when the state is `idle`, `done` or `waiting`. In `running` and `permission` they stay queued: typing then would land in a tool run or answer a permission prompt.
- The worker rechecks the session before typing and puts the switches back in the queue (`Requeue`) if a turn started while it waited. The daemon calls `Dispatch` after `session.switch` and after every hook, so a switch queued while `running` goes out on the next `Stop` or waiting notification. The typing runs on a worker: `app.SendSwitches` pastes the command with bracketed paste, waits 150 ms, then presses Enter. A failure drops the switch and raises the warning.
- The command is `/model <value>` or `/effort <value>` (`domain.SwitchCommand`). Codex gets `/model` and a walk of its picker instead; see ADR 0034.
- Confirmation is event based, not timed. `Session.Report` checks each sent switch against the next report that says something about its kind. A match clears it. A report that still shows the old value keeps the switch and sets `SwitchWarning`, and a later matching report clears both. A model matches when the reported name equals the requested one or its first word does, since Claude reports `Opus 4.7` for `opus` but `gpt-5` must not match `gpt-5-codex`; an effort matches exactly, ignoring case.
- The sidebar shows `→ value` on the right of the detail row for each unconfirmed switch, and `!` when `SwitchWarning` is set. The new value itself appears as soon as the report lands, so it follows the harness's confirmation by one diff.
- Per-harness defaults are `[defaults.claude]` and `[defaults.codex]` tables (`model`, `effort`) in `$CREW_HOME/config.toml`, read by `tui.LoadDefaults`. The new-session dialog pre-fills model and effort from the chosen harness's table, and follows them when the harness field changes unless the user edited the value.

## Why

- Sending the harness's own slash command keeps crew out of the harness's config and works on a live session. The cost is that success is only visible through the harness's own report.
- Warning on the next report, rather than on a timer, matches when each harness can tell us. Claude refreshes its status line right after the command. Codex writes the new model and effort to the rollout on its next turn, so a timer would raise false warnings.
- Queued switches live on the session so they are stored and shown like any other session state, and survive a TUI restart.

## Limits

- The pick lists are fixed in `domain.SwitchChoices`, not read from the harness.
- The recheck narrows the window between the state check and the paste to the 150 ms settle; it does not close it.
- Codex was not supported here: in 0.159 `/model` opens an interactive picker and there is no `/effort`. [ADR 0034](0034-codex-model-picker-switching.md) adds it by driving that picker with keys.
- A draft typed in the harness's input box is not cleared, so the command is appended to it.
- Checked by hand in isolated tmux servers against a fake `claude` that reports through the real `crew hook` and `crew statusline`. Real Claude and Codex were not driven: both need credentials, and the rule for this repo is temporary config dirs only.
