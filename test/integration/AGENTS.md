# test/integration

`-tags integration` tests that run the daemon with its real adapters. The daemon package itself must not exec. CI runs them with all other integration-tagged packages.

- `go test -tags integration -run CodexPicker ./test/integration/` controls a fake Codex model picker (`testdata/fake-codex-picker.py`, needs `python3`) in a real tmux pane. See [ADR 0034](../../docs/adr/0034-codex-model-picker-switching.md).
- `TestNvimPluginSpecs` runs `nvim/test/spec.lua` headless with `nvim --clean` and temporary XDG dirs. Never point plugin tests at `~/.config/nvim`. See [nvim/](../../nvim/AGENTS.md).
- `CleanupExec`, `Ports`, `ReviewSend` and worktree detection also have end-to-end cases here. One `ReviewSend` case pastes into a real tmux pane. Their feature notes are in [internal/daemon/](../../internal/daemon/AGENTS.md).
