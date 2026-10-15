# ADR 0040: First-run walkthrough

Status: accepted, 2026-10-01.

## Decision

- **When.** The sidebar asks the daemon for `onboarding.status` once, on its first snapshot. Until `$CREW_HOME/onboarded` exists, and only when `domain.OnboardingNeeded` says something is left (no harness set up, or nvim on PATH and not configured), it opens the walkthrough as a centred popup (`crew tui --setup`, run through `client.popup` like the new-session dialog in ADR 0037). On the first run the sidebar can start before tmux has attached a client, so a failed popup is retried twice, 400 ms apart, and then the walkthrough opens inline in the sidebar. `S` in the sidebar and `crew setup` with no arguments open it again at any time. Finishing it, or pressing `esc` at any step, writes the marker. A machine with nothing left gets the marker written silently, so existing users never see it.
- **Steps.** Agents (pick Claude Code, Codex or both; the ones already set up are picked) → Claude Code → Codex → Neovim → Done. `domain.OnboardSteps`, `NextOnboardStep`, `DefaultOnboardPicks`, `HarnessOffer` and `NvimOfferFor` decide the steps and what each one offers. They are pure and table-tested.
- **Harness steps.** Each one shows the state, the file it changes, what changes, the backup and the undo command (`crew setup <harness> --remove`). `⏎` installs and `s` skips. The Codex step also shows the one-time trust step (`domain.CodexTrustStep`, which `crew setup codex` prints too). A config the adapter cannot parse is reported and gets no install offer.
- **Same check as `crew setup`.** `claude.Installed` and `codex.Installed` run Setup's merge in memory and report whether it would change nothing, so the walkthrough and the CLI cannot disagree. Install calls the same `claude.Setup` and `codex.Setup`, so it stays idempotent, backs up first and can be undone.
- **nvim (optional).** `adapters/onboard` looks `nvim` up on `PATH` and scans the whole `$XDG_CONFIG_HOME/nvim` tree (`*.lua`, `*.vim`, at most 400 files) for `require('crew')` or an `crew/nvim` path, so lazy.nvim specs in `lua/plugins/`, kickstart, NvChad and `init.vim` setups count as configured. The plugin dir is the first of `$XDG_DATA_HOME/crew/nvim` (default `~/.local/share/crew/nvim`, where the install script puts it) and a source checkout's `nvim/` next to `bin/`. When the plugin is not configured, the step shows the exact file it will write and its contents; `⏎` writes `$XDG_CONFIG_HOME/nvim/plugin/crew.lua` (creating the dirs), `s` skips. That file prepends the plugin dir to the runtimepath, sources the plugin's commands and calls `require('crew').setup({})`. nvim sources a config's `plugin/` files whatever else the config uses, so no file of the user's is edited and nothing needs a backup. `crew setup nvim [--remove]` does the same from the CLI; remove deletes the file only when its first line is crew's marker, and install refuses to overwrite a file it did not write. Without nvim the step says it is optional, what it adds and how to install it (`brew install neovim`, or the package manager), and `⏎` just goes on.
- **No nvim elsewhere.** `app.Editor.Installed` (`exec.LookPath` in `adapters/nvim`) is checked before `e` or `o` starts an nvim pane; without it the daemon answers `unavailable` with how to install Neovim, and the sidebar shows that instead of an empty slot. The shell keys never need nvim.
- **Layers.** Detection and installs run in `adapters/onboard`, behind the `app.Onboarder` port. The daemon serves `onboarding.status`, `onboarding.install` and `onboarding.finish` on the asking connection, never on the loop. The TUI talks only to rpc and renders from the reply.
- **Releases.** Archives ship `nvim/lua` and `nvim/plugin`, and `scripts/install.sh` copies them to `${XDG_DATA_HOME:-~/.local/share}/crew/nvim`. `CREW_DOWNLOAD_BASE` points the script at another archive host, such as `python3 -m http.server` in a `goreleaser release --snapshot` `dist/`, to try it without a release.

## Why

- A release user has no source checkout, so the snippet has to point at a path the install creates.
- The marker is a file in `CREW_HOME`, so a temp home (tests, `make dev`) always starts fresh. The walkthrough never needs the state database.
- A file of crew's own in `plugin/` works with every config style and plugin manager, and undo is deleting it.

## Limits

- Hooks are written for the daemon's own binary path. A binary that moves later shows as "not set up" until setup runs again.
- The nvim check is a text search, so a config that loads the plugin some other way is offered the file anyway. It can be skipped.
- A config that disables loading `plugin/` files (`--noplugin`, or lazy.nvim with `performance.rtp.reset` and a custom `paths` that leaves the config dir out) does not load the file.
