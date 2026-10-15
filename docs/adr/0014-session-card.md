# ADR 0014: Session card

Status: accepted, 2026-09-29. Amended 2026-10-04: the sidebar card was removed; the builder stays.

## Decision

- The daemon keeps each session's last 20 hook events (`domain.SessionEvent`: kind, time, tool, one-line detail, and the permission, notification or assistant text). `domain.SessionEventFromHook` reduces a payload to that; text is capped at 4000 bytes.
- The store persists them in `session_events` (migration 0003), appended by `PutEvent` in the same write-behind batch as everything else and pruned to the newest 20 per session in that transaction. They load back into the snapshot on start.
- A hook publishes one diff carrying both the session and its event (`Diff.Event`, alongside `Diff.Session`), and the snapshot has `State.Events`. Ordinary diffs still set one field.
- `domain.BuildSessionCard(task, session, worktrees, events)` is the only place the card is derived, and it reads nothing but those arguments. It gives the task label, PR chips from the session's worktrees, the last 3 `pre_tool_use` calls (newest first), and the waiting reason:
  - `permission`: the latest permission request's text.
  - `waiting`: the last assistant message's closing paragraph if it ends in `?`, else the notification message.
  - `done`: the closing question, if there is one.
  - Only events after the last user prompt count. The text is verbatim, cut to 3 lines with a trailing `…`.
- The TUI no longer draws the card in the sidebar (removed in #203: the panel above the footer was not useful, and the session list gets its rows back). `BuildSessionCard` still feeds the review's agent column and `crew pr`.

## Why

- "Derives only from stored events" means the card must survive a daemon restart and be testable from a fixture log, so the events are stored and the builder is pure.
- Keeping 20 per session bounds memory, disk and snapshot size; the card needs 3 actions and one prompt.
- One diff for session and event means a subscriber never shows a state without the event that explains it.
- The main pane belongs to the agent, so "the session header area" is the sidebar, next to the selected row.

## Limits

- `Task` has no URL field, so the card shows the task's ref (for example `#42`), not a link, until tasks carry one.
- A permission prompt longer than the pane is cut at the pane's width per line, not wrapped, so the 3-line rule holds.
