# ADR 0008: TUI shell and client layout

Status: accepted, 2026-09-29.

## Decision

- The daemon owns the client layout. `crew` calls `client.open`, gets back the tmux attach argv, and `exec`s it. The tmux adapter builds that argv, so tmux flags stay in one package.
- One client window per daemon, reused while it exists. `client.focus_main` needs no argument because there is only one.
- The sidebar pane is a fixed 48 columns, kept there by a `window-resized` hook.
- The top bar is drawn inside the sidebar pane, not across the full width as in the mockup. A tmux pane cannot be L-shaped, and a separate top pane would cost a row of the agent's pane plus a second TUI process.
- Golden snapshots store the ANSI-stripped frame. Colors are checked by tests of their own, not by goldens.
- The renderer runs at 120 fps.

## Why

- The TUI may not run tmux (depguard), and `cmd/crew` should not duplicate adapter flags.
- tmux splits a window resize across both panes, so a one-off `resize-pane` does not hold.
- Stripped goldens are readable in review and do not change when a color profile or style escape changes.
- Measured in a nested tmux, keypress to changed frame was 25.5 ms p95 at 60 fps and 17.1 ms at 120 fps, against a 12.6 ms baseline for the same harness around `cat`. The model itself takes 0.5 ms (benchmark).

## Rejected

- **Top bar as its own tmux pane:** see above.
- **TUI resizing its own pane:** it would need tmux on the render path.
