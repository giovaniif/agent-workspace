# ADR 0025: Focus return key and the empty main slot

Status: accepted, 2026-09-30.

## Decision

- The crew tmux config binds `ctrl+\` in the root table (`bind-key -n C-\\ select-pane -t :.0`), right after `unbind-key -a`. From an agent pane it moves keyboard focus to the sidebar, pane 0 of the client window. The key is `tmux.FocusSidebarKey`.
- The binding lives only in the config file the adapter writes for `tmux -L crew`. The user's default server and `~/.tmux.conf` are never read or written; an integration test checks the default server has no such binding.
- `ctrl+\` is a raw control byte with no readline meaning, and neither Claude Code nor Codex binds it. Keys with a modifier that terminals send inconsistently (`alt+<key>` needs "Option as Meta" on macOS) were not used. The agent no longer receives that one key.
- When `session.end` ends the session that is in view, the daemon shows the next session in sidebar order that still has a pane, wrapping to the top (`domain.NextInView`), and marks it in view. Keyboard focus stays in the sidebar.
- With no session left to show, `ClientHost.EnsureSlot` gives the slot a pane that prints "No session in view. Press n to start one.". `Show` uses the same pane when it has to recreate a slot.
- Ending a session that is not in view leaves the slot alone.
- An agent that exits by itself makes tmux drop its pane. A daemon worker (`watchMainSlot`, every 2 s, off the event loop) asks `ClientHost.SlotHasPane`, one `list-panes` call. When the slot has lost its pane it ends the session in view the same way `session.end` does, so the next session or the empty-state pane fills the slot. An unreadable window counts as "has a pane". Hook events (`SessionEnd`) were not used: Codex may not send one and a crash sends none.
- The TUI help overlay lists the key.

## Why

- Sessions and the sidebar share one tmux window and the server unbinds every key, so without a binding a user in an agent pane has no way back except a mouse.
- The next session in sidebar order is where the eye lands once the ended row's position is reused.

## Rejected

- **A tmux prefix key:** the config sets `prefix None` so nothing the agent needs is swallowed; a prefix would bring that back.
- **Rebinding on the user's server:** violates "leave the user's setup alone".
