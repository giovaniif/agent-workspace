# internal/adapters/tmux

The only code that runs tmux; only `internal/daemon` may import it. See [ADR 0003](../../../docs/adr/0003-tmux-terminal-host.md).

- It drives a dedicated server (`tmux -L agentws`, or `AGENTWS_TMUX_SOCKET`) with its own config, never the user's server. Panes are parked in their own windows and `swap-pane` puts one in the client's main slot.
- Keys: `ctrl+\` (`FocusSidebarKey`) moves focus from an agent pane to the sidebar, bound on the `agentws` server only ([ADR 0025](../../../docs/adr/0025-focus-return-key.md)). The config binds `M-t` (closes the shell popup); `C-h/j/k/l` are not bound so they reach every pane (the nvim plugin navigates at nvim's edge).
- tmux reads `-f` only when a server starts, so the first command a `Host` runs is `source-file` of the freshly written config: a server that outlived an upgrade picks up new settings (the mouse, say) on the next daemon start. Its exit status is ignored, because on a running server `unbind-key -a` always reports a missing prefix table. With no server yet, `-f` covers the next one. Only a canceled `source-file` is retried.
- `LastInput` (`app.TerminalActivity`) is the newest `#{client_activity}` of `list-clients`: when someone last typed in a client attached to the `agentws` server. No client, or no server, is the zero time and no error. The daemon samples it for Web Push presence (see "Web Push" in [internal/daemon/](../../daemon/AGENTS.md)). `go test -tags integration -run Presence ./internal/adapters/tmux/` attaches a control-mode client (`tmux -C`), which needs no terminal.
- `display-popup` (the `n` dialog, the `T` shell) goes to the attached client with the newest `#{client_activity}`, the one that just took the keypress. A stale client left attached in another terminal must not get it.
- Pane title strips are set through `SetTitle` ([ADR 0038](../../../docs/adr/0038-pane-title-strips.md)).
- Tests are `-tags integration` against real tmux, each with a unique socket. `go test ./... -run Shell -tags integration` covers the split, popup and key pass-through.

## Mouse

The written config turns `mouse on` and spells out the mouse bindings, since `unbind-key -a` drops tmux's own: click selects the pane and passes the click on, border drag resizes, wheel and drag go to programs that take the mouse (`mouse_any_flag`) or enter copy-mode. `Config.NoMouse` (from `[ui] mouse = false`, read by `daemon.LoadNoMouse`) writes `mouse off` instead. Keep ` \; ` with spaces in chained bindings: `\;send` breaks key pass-through. `go test -tags integration -run Mouse ./internal/adapters/tmux/`. See [ADR 0042](../../../docs/adr/0042-mouse.md).
