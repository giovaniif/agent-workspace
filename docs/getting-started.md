# Getting started

## The setup screen

Run `crew`. The first time, a setup screen opens on top of the sidebar:

1. **Agents.** Select Claude Code, Codex or both (`↑/↓`, `space`, `⏎`). Each agent shows if its hooks are already set up.
2. **Claude Code.** The screen shows these items:
   - The file that it changes (`~/.claude/settings.json`, or `$CLAUDE_CONFIG_DIR`).
   - What it adds: the crew hooks and a status-line wrapper that still runs your status line.
   - Where the backup goes.
   - How to undo it (`crew setup claude --remove`).

   `⏎` installs, `s` skips.
3. **Codex.** The same for `~/.codex/hooks.json` (or `$CODEX_HOME`). Codex then needs a one-time trust step. Start `codex` and accept the review prompt for the new hooks, or trust them in `/hooks`.
4. **Neovim (optional).** This step applies if `nvim` is on `PATH` and no part of your nvim config configures the plugin yet.
   - The screen shows the file that it will write: `~/.config/nvim/plugin/crew.lua`. This file loads the plugin that the install script put in `${XDG_DATA_HOME:-~/.local/share}/crew/nvim`.
   - `⏎` writes it, `s` skips.
   - It does not edit your own files (init.lua, init.vim, lazy.nvim specs). `crew setup nvim --remove` deletes the file.
   - Without nvim, the step tells how to install it (`brew install neovim`), and `⏎` continues. All keys except `e` and `o` work without nvim.
5. **Done.** Press `⏎` to close the setup screen, then press `n` to start your first session.

At each step, `esc` skips the remaining steps. After you finish or skip, the screen does not open again automatically. It never opens when a harness is already set up and nvim has nothing more to configure. To open it again, press `S` in the sidebar or run `crew setup`. `crew setup claude`, `crew setup codex` and `crew setup nvim` do the same installs without the screen.

## Mates in Slack

A mate is a named personality of crew in Slack. Each mate is its own Slack app, so it has its own name and avatar. The app uses Socket Mode. Your server needs no public URL and no open port. The app is not in the Slack Marketplace, so Slack does not review it. See [ADR 0053](adr/0053-mates.md).

To add a mate:

1. Run `crew mate add <name>`. The command saves the mate, prints the Slack app manifest and the link to make the app from it. It opens the link when it can. Add `--no-open` to only print it. Add `--agent codex` or `--prompt "..."` to set those fields.
2. In Slack, make the app from the manifest and install it in your workspace. A workspace admin can require an approval for the install. If so, ask the admin to approve the app.
3. Copy the bot token (`xoxb-`) from OAuth & Permissions.
4. In Basic Information, make an app-level token with the scope `connections:write`. Copy it (`xapp-`).
5. Paste the two tokens when the command asks. The terminal does not show them.

crew keeps the tokens in files with mode 600 under `$CREW_HOME/secrets/`. They are never in `config.toml`, a log or an answer of the daemon. If you gave a wrong token, run `crew mate tokens <name>`. To remove the tokens, run `crew mate tokens <name> --clear`.

## Build and test

You need Go (the version in `go.mod`) and `golangci-lint` v2. For `make mutate`, you also need `gremlins`.

```sh
make build             # ./bin/crew
./bin/crew version
./bin/crew debug seed 3 && ./bin/crew   # the sidebar with 3 fake sessions
./bin/crew workspace add ~/src/api && ./bin/crew new "fix the flaky test"   # or press n in the TUI
make test              # go test ./...
make lint              # golangci-lint + scripts/lint-comments
make e2e               # testscript suite in test/e2e
make bench             # benchmarks for the performance budgets
make mutate            # gremlins on internal/domain and internal/app
```

Integration tests use `-tags integration` and need `git` and `tmux`.

## Shell and nvim

In the sidebar:

- `t` shows a shell in the worktree below the agent pane of the selected session. `t` again hides it.
- `T` opens the shell as a popup. `M-t` closes it.
- `e` swaps the nvim of the session into the main area, and back.

In the review, `o` opens the file in that nvim, at the top line of the diff. In nvim, `C-h/j/k/l` move between its splits. At the edge, they move to the adjacent pane. All other panes receive those keys with no change.

The setup screen shows the exact lines for your install. To set it up by hand:

1. Put the plugin dir on the runtimepath, with your plugin manager or with `vim.opt.rtp:prepend('<dir>')`. The dir is `${XDG_DATA_HOME:-~/.local/share}/crew/nvim` from a release, or the repo's `nvim/` in a checkout.
2. Call `require('crew').setup({})`.

The plugin needs `crew` on `PATH`. It works in the nvim that the `e` key starts, which has `CREW_SESSION` set.

- `:CrewDiff [last_turn|uncommitted|branch]` opens a review scope of the session in diffview. The default is the scope that the review pane has open.
- `:'<,'>CrewComment [text]` adds the selected lines as a draft review comment.
- `require('crew').setup({ bin = 'crew', tmux = 'tmux', navigate = true })`. With `navigate = false`, the plugin does not change `C-h/j/k/l` in nvim.

## Mouse

Click a session to select it. Click it again to go to its agent pane. You can click these items:

- Key hints and buttons at the bottom of each screen.
- Picker choices, dialog fields and worktree rows.
- Review files, and scope and worktree chips.
- Diff lines. Drag over lines to select a range, then press `c` to comment.

The wheel moves through lists. In tmux, a click on a pane focuses it, and a drag on a border resizes. The wheel scrolls the history of an agent (`q` stops this). To select text with the terminal's own selection, hold Shift while you drag (Option in iTerm2 and Terminal.app). To turn off all mouse support:

```toml
# $CREW_HOME/config.toml
[ui]
mouse = false
```

