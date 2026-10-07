# internal/adapters/tmux

This is the only code that runs tmux. Only `internal/daemon` can import it. See [ADR 0003](../../../docs/adr/0003-tmux-terminal-host.md).

- It controls a dedicated server (`tmux -L agentws`, or `AGENTWS_TMUX_SOCKET`) with its own config. It never uses the user's server. Panes are parked in their own windows. `swap-pane` puts one pane in the main slot of the client.
- Keys: `ctrl+\` (`FocusSidebarKey`) moves focus from an agent pane to the sidebar. It is bound only on the `agentws` server ([ADR 0025](../../../docs/adr/0025-focus-return-key.md)). The config binds `M-t`, which closes the shell popup. `C-h/j/k/l` are not bound, so they get to all panes. The nvim plugin moves to the next pane at the edge of nvim.
- tmux reads `-f` only when a server starts. Thus the first command that a `Host` runs is `source-file` of the newly written config. A server that continued to run through an upgrade then gets new settings (for example, the mouse) on the next daemon start.
  - The `Host` ignores the exit status, because on a running server `unbind-key -a` always reports a missing prefix table.
  - If there is no server yet, `-f` applies to the next one.
  - The `Host` tries a `source-file` again only if it was canceled.
- `LastInput` (`app.TerminalActivity`) is the newest `#{client_activity}` of `list-clients`: the last time that a person typed in a client attached to the `agentws` server. With no client or no server, it is the zero time and no error. The daemon samples it for Web Push presence (see "Web Push" in [internal/daemon/](../../daemon/AGENTS.md)). `go test -tags integration -run Presence ./internal/adapters/tmux/` attaches a control-mode client (`tmux -C`), which needs no terminal.
- `display-popup` (the `n` dialog, the `T` shell) goes to the attached client with the newest `#{client_activity}`: the client that just got the keypress. An old client that stays attached in a different terminal must not get it.
- `SetTitle` sets the pane title strips ([ADR 0038](../../../docs/adr/0038-pane-title-strips.md)).
- Tests use `-tags integration` and real tmux, each with its own socket. `go test ./... -run Shell -tags integration` covers the split, popup and key pass-through.

## Native clients

`OpenNative` implements `client.native` (ADR 0049, spike #216). Each call does these steps:

1. It makes a session `agentws-native-<pid>-<n>` on the `agentws` server.
2. It links into it all parked windows (name `pane-*` in the `agentws` session) and all TUI windows (`client-*`).
3. It returns the control-mode argv.

`Create`, `HideBelow` and `OpenClient` link new windows into all native sessions. Thus an attached client gets `%window-add`. A killed window leaves automatically. A pane that the TUI swaps into its slot stays in the native session, in the window of the TUI. Pane ids do not change with `swap-pane`, so clients use the pane id as the key of a view, not the window.

- A TUI window has `window-size manual`. Thus the size of a client, native or not, does not resize it.
  - The `client-attached` and `client-resized` hooks of the `agentws` session resize the current window of the session to the client size. The current window is the one that the terminal shows. `AttachCommand` targets `=agentws:@slot`, because `@slot` alone can select a native session.
  - Then the hooks run the `window-resized` hook of the window again (`resize-window` alone does not start it). This keeps the sidebar pinned.
  - Control-mode `refresh-client -C` does not start `client-resized`. Thus the test of terminal sizes attaches through an outer tmux.
- A session gets `destroy-unattached` from a `client-attached` hook. If it got it at creation, the session would stop immediately. Thus the session stops when its client detaches or stops. After a minute, the next call removes sessions that were never attached.
- tmux does not send mouse and cursor modes again to a new client. Thus the reply has them from format variables, with the real size of each pane for letterboxing.
- These items are for the app (#227):
  - Match replies by the `%begin` number.
  - Kill its `tmux -C` process when it drops the transport. An orphaned process blocks the server.
  - `refresh-client -C` sizes.
  - `pause-after` with `%extended-output` and `refresh-client -A %p:continue`.
- `go test -tags integration -run Native ./internal/adapters/tmux/`.

## Mouse

The written config sets `mouse on` and writes all the mouse bindings, because `unbind-key -a` removes the tmux bindings:

- A click selects the pane and sends the click to it.
- A border drag resizes.
- The wheel and drag go to programs that use the mouse (`mouse_any_flag`), or start copy-mode.

`Config.NoMouse` (from `[ui] mouse = false`, read by `daemon.LoadNoMouse`) writes `mouse off`. In chained bindings, keep ` \; ` with spaces: `\;send` breaks key pass-through. `go test -tags integration -run Mouse ./internal/adapters/tmux/`. See [ADR 0042](../../../docs/adr/0042-mouse.md).
