# nvim

The Lua plugin. It talks to the daemon only through `agentws` CLI calls. The daemon controls a long-lived nvim for each session through `nvim --listen`. See [ADR 0029](../docs/adr/0029-shell-and-nvim.md) and the shell and nvim notes in [internal/daemon/](../internal/daemon/AGENTS.md).

- **Layout.** `lua/agentws` and `plugin/agentws.lua`: `:AgentwsComment`, `:AgentwsDiff [scope]`, `setup{bin, tmux, navigate}`. The plugin calls `agentws review comment` and `agentws review scope`, then opens `:DiffviewOpen`. `agentws review scope` gives JSON with the path, base commit and files of each worktree.
- **Navigation.** tmux does not bind `C-h/j/k/l`, so these keys get to all panes. At the edge of nvim, the plugin moves to the adjacent tmux pane.
- **Tests.** `TestNvimPluginSpecs` (`test/integration`) runs `test/spec.lua` with `nvim --clean` and temporary XDG dirs. Never point plugin tests at `~/.config/nvim`. The nvim-only tests skip if `nvim` is not installed. CI installs it.
- **By hand.** Use a temporary `AGENTWS_HOME`, `AGENTWS_TMUX_SOCKET` and `XDG_CONFIG_HOME`. In that `XDG_CONFIG_HOME`, write an `nvim/init.lua` that prepends `nvim/` to the runtimepath and calls `require('agentws').setup({})`.
- **Releases** contain `nvim/lua` and `nvim/plugin`. `scripts/install.sh` copies them under `$XDG_DATA_HOME/agentws/nvim`. `agentws setup nvim` points the user's config at them (see [internal/adapters/onboard/](../internal/adapters/onboard/AGENTS.md)).
