# internal/adapters/tmux

The only code that runs tmux; only `internal/daemon` may import it. See [ADR 0003](../../../docs/adr/0003-tmux-terminal-host.md).

- It drives a dedicated server (`tmux -L agentws`, or `AGENTWS_TMUX_SOCKET`) with its own config, never the user's server. Panes are parked in their own windows and `swap-pane` puts one in the client's main slot.
- Keys: `ctrl+\` (`FocusSidebarKey`) moves focus from an agent pane to the sidebar, bound on the `agentws` server only ([ADR 0025](../../../docs/adr/0025-focus-return-key.md)). The config binds `M-t` (closes the shell popup); `C-h/j/k/l` are not bound so they reach every pane (the nvim plugin navigates at nvim's edge).
- tmux reads `-f` only when a server starts, so the first command a `Host` runs is `source-file` of the freshly written config: a server that outlived an upgrade picks up new settings (the mouse, say) on the next daemon start. Its exit status is ignored, because on a running server `unbind-key -a` always reports a missing prefix table. With no server yet, `-f` covers the next one. Only a canceled `source-file` is retried.
- `LastInput` (`app.TerminalActivity`) is the newest `#{client_activity}` of `list-clients`: when someone last typed in a client attached to the `agentws` server. No client, or no server, is the zero time and no error. The daemon samples it for Web Push presence (see "Web Push" in [internal/daemon/](../../daemon/AGENTS.md)). `go test -tags integration -run Presence ./internal/adapters/tmux/` attaches a control-mode client (`tmux -C`), which needs no terminal.
- `display-popup` (the `n` dialog, the `T` shell) goes to the attached client with the newest `#{client_activity}`, the one that just took the keypress. A stale client left attached in another terminal must not get it.
- Pane title strips are set through `SetTitle` ([ADR 0038](../../../docs/adr/0038-pane-title-strips.md)).
- Tests are `-tags integration` against real tmux, each with a unique socket. `go test ./... -run Shell -tags integration` covers the split, popup and key pass-through.

## Native clients

`OpenNative` backs `client.native` (ADR 0049, spike #216). Each call makes a session `agentws-native-<pid>-<n>` on the `agentws` server, links into it every parked window (name `pane-*` in the `agentws` session) and every TUI window (`client-*`), and returns the control-mode argv. `Create`, `HideBelow` and `OpenClient` link new windows into every native session, so an attached client gets `%window-add`; a killed window leaves by itself. A pane the TUI swaps into its slot stays in the native session, in the TUI's window. Pane ids survive `swap-pane`, so clients key views by pane id, not window.

- A TUI window has `window-size manual`, so no client's size, native or not, resizes it. The `agentws` session's `client-attached` and `client-resized` hooks resize the session's current window (the one that terminal shows; `AttachCommand` targets `=agentws:@slot`, since `@slot` alone can pick a native session) to its size, then rerun the window's `window-resized` hook (`resize-window` alone does not fire it), which keeps the sidebar pinned. Control-mode `refresh-client -C` fires no `client-resized`; the test of terminal sizes attaches through an outer tmux instead.
- A session gets `destroy-unattached` from a `client-attached` hook (set at creation, it would kill the session at once), so it goes when its client detaches or dies. Sessions never attached are reaped by the next call after a minute.
- Mouse and cursor modes are not replayed to a new client, so the reply carries them from format variables, with each pane's real size for letterboxing.
- Left to the app (#227): matching replies by the `%begin` number, killing its `tmux -C` process when it drops the transport (an orphaned one wedges the server), `refresh-client -C` sizes, and `pause-after` with `%extended-output` and `refresh-client -A %p:continue`.
- `go test -tags integration -run Native ./internal/adapters/tmux/`.

## Mouse

The written config turns `mouse on` and spells out the mouse bindings, since `unbind-key -a` drops tmux's own: click selects the pane and passes the click on, border drag resizes, wheel and drag go to programs that take the mouse (`mouse_any_flag`) or enter copy-mode. `Config.NoMouse` (from `[ui] mouse = false`, read by `daemon.LoadNoMouse`) writes `mouse off` instead. Keep ` \; ` with spaces in chained bindings: `\;send` breaks key pass-through. `go test -tags integration -run Mouse ./internal/adapters/tmux/`. See [ADR 0042](../../../docs/adr/0042-mouse.md).
