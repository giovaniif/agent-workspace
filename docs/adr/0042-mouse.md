# ADR 0042: Mouse support

Status: accepted, 2026-10-01.

## Decision

- **On by default, one switch.** `[ui] mouse = false` in `$CREW_HOME/config.toml` turns it off in the TUI (`tui.LoadMouse`, which sets `Options.NoMouse`) and in the crew tmux server (`daemon.LoadNoMouse`, which sets `tmux.Config.NoMouse`). A server already running keeps its config until it restarts.
- **TUI: no zone library.** Views stay pure strings. Each screen's layout function returns, beside its lines, an owner per row (`s:<session>`, `p:<choice>`, `f:<field>`, `fc:<column width>`, `d:<worktree>`). `View` ignores them. On a click, `Update` calls the same layout function again and looks the row up. The review pane is laid out in fixed bands (rail, chips, tree, diff), so a click there is placed by arithmetic, and the tree returns the file index per row. bubblezone was not used: it marks every zone in every frame and scans the output each render, which costs every frame for something that happens on a click.
- **What a click does.**
  - Sidebar: a click on any row of a card selects that session and focuses its agent pane (`enter`), so one click lands in the agent's input. A task header row does the same for that task's first session.
  - Key hints and buttons: a click on `<key> <what>` fires that key through the normal key path, so the mouse never has behavior a key does not. Hints are split by two spaces or ` · `; `a/b` picks the half under the pointer; `⏎ ␣ ⇥ esc ← →` and `ctrl+x`/`^x` map to their keys; digits never count, so `2 sessions` is not a hint. Only the bottom block of a screen (its footer), help rows and rows that name `esc` (button rows) are read as hints, so clicking an agent's text in the card never fires a key.
  - While a text field is focused (new-session dialog, launcher, rename, review comment), only non-printing keys fire from a click, so a click never types into the field.
  - Picker: a click applies that choice. Dialog: a click focuses the field (the side-by-side pickers by column). Worktrees: a click selects the row.
  - Review: a click on a file opens it; on a scope or worktree chip switches to it; on a diff line places the cursor there. Dragging from a diff line marks a range, as `V` does, ready for `c`.
- **Wheel.** It moves the cursor of the current list (sidebar selection, picker, worktrees, review line by 3), and lists scroll with the cursor as they do for `j`/`k`. It does nothing in text inputs, help or the walkthrough.
- **tmux.** The config's `unbind-key -a` removes tmux's own mouse bindings, so they are spelled out: a click selects the pane and is passed on; dragging a border resizes; a drag or the wheel goes to the program when it asked for the mouse (`mouse_any_flag`) or is in copy-mode; otherwise a drag or a wheel-up enters copy-mode, so agent history scrolls and selects. A wheel-down outside copy-mode is already at the bottom, so it only goes to a program that asked for it, as in tmux's own defaults. A drag in copy-mode copies on release (`copy-selection-and-cancel`, which reaches the system clipboard where the terminal allows OSC 52). `q` or `Esc` leaves copy-mode. Only the crew server's config changes.

## Why

- Hit-testing on the click keeps the render budget untouched; `BenchmarkMouseClick` measures the click (about 0.4 ms with ten sessions), and `BenchmarkKeypressToFrame` is unchanged.
- Reusing the key path means every click has the tests and the behavior its key already has.

## Limits

- With the mouse on, the terminal no longer selects text on a plain drag. Hold Shift while dragging (Option in iTerm2 and Terminal.app) for the terminal's own selection, or turn the mouse off.
- A drag in the review selects lines in the visible part of the diff; it does not scroll the diff while dragging.
- The walkthrough has only its buttons clickable, not its pick list; the launcher and rename prompt have only their hints.
