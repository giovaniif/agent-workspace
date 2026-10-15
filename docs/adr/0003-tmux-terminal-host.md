# ADR 0003: tmux terminal host

Status: accepted, 2026-09-29.

## Decision

- All tmux calls live in `internal/adapters/tmux`. Every call is `tmux -L <socket> -f <config>` (socket `crew` by default), with the `TMUX` env vars stripped. The config is written by the adapter: status line off, prefix `None`, all default key bindings removed except the focus-return key (ADR 0025), `remain-on-exit off`.
- One tmux session (`crew`). Every pane made by `Create` lives in its own parked window and is tagged with the pane option `@crew`, so `List` ignores the TUI and placeholder panes.
- `OpenClient` makes one window per client: the TUI's pane at index 0, and a placeholder at index 1 that is the main slot. `Show` is a single `swap-pane -d -s <pane> -t <window>.1`. If the slot pane is gone (the shown pane exited), `Show` splits a new placeholder and retries.
- `SendText` loads the text into a named tmux buffer over stdin and pastes it with `paste-buffer`. Bracketed paste adds `-p -r`, so the pane's app gets the paste markers if it asked for them and newlines stay newlines. Without it, newlines become carriage returns.
- `Alive` is "the pane exists and is not dead". With `remain-on-exit off` a dead pane disappears, so a missing pane counts as not alive.
- `app.ReconcilePanes` runs on daemon start. It idles every session whose pane is missing or dead, and changes nothing if tmux cannot be listed.
- Enforcement is `depguard`: `domain`, `app`, `tui`, `rpc` and `daemon` may not import `os/exec`, and only `daemon` (and the adapter itself) may import `internal/adapters/tmux`. Other adapters can still exec, so a stray tmux call there is caught in review, not by lint.
- Integration tests and the switch benchmark are behind `-tags integration` and use a unique socket per test. CI runs them with `go test -tags integration ./internal/adapters/tmux/...`.

## Why

- `swap-pane` is one exec (about 3 ms p95 measured), well under the 60 ms budget, and it keeps the running process attached to its pane instead of re-attaching a client.
- Tagging panes with an option keeps `List` correct without a naming convention that a rename could break.

## Rejected

- **`join-pane`/`break-pane` for switching:** two calls and it changes window structure.
- **Banning the string `"tmux"` outside the adapter:** depguard and forbidigo cannot match string arguments, and a custom check would be one more script for a rule the import ban already mostly covers.
