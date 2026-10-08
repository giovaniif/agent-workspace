# agentws (working name)

A terminal workspace that runs Claude Code and Codex sessions in parallel. It creates and cleans up git worktrees for you. It lets you review the changes of agents in a local, PR-style pane.

**Status:** alpha. Builds are pre-releases on [GitHub Releases](https://github.com/giovaniif/agent-workspace/releases). Expect problems and breaking changes between alphas. The screens below are design mockups. Their sources are in [docs/design](docs/design).

## Install

Builds are available for macOS (arm64, amd64) and Linux (amd64). You do not need a Go toolchain. You need `tmux`, `git` and the GitHub CLI (`gh`, signed in). You also need Claude Code, the Codex CLI or both.

Optional, on macOS: `brew install terminal-notifier`. If it is on `PATH` when the daemon starts:

- Each session keeps one banner. A newer banner replaces it.
- A click on a banner shows your terminal on that session.
- A banner goes away when the session continues.

Without it, banners go through `osascript`.

Install the newest release (alphas included):

```sh
curl -fsSL https://raw.githubusercontent.com/giovaniif/agent-workspace/main/scripts/install.sh | sh
```

The script does these steps:

1. It downloads the archive for your OS and CPU from GitHub Releases.
2. It checks the archive against `checksums.txt`.
3. It puts the `agentws` binary in `~/.local/bin`.
4. It puts the nvim plugin in `~/.local/share/agentws/nvim` (`$XDG_DATA_HOME/agentws/nvim` if that is set).

Two environment variables change this behavior:

```sh
# a specific release
curl -fsSL https://raw.githubusercontent.com/giovaniif/agent-workspace/main/scripts/install.sh | AGENTWS_VERSION=v0.1.0-alpha.1 sh
# another directory
curl -fsSL https://raw.githubusercontent.com/giovaniif/agent-workspace/main/scripts/install.sh | AGENTWS_INSTALL_DIR=/usr/local/bin sh
```

If the install directory is not on your `PATH`, the script tells you. Add it in your shell profile (`~/.zshrc` or `~/.bashrc`):

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Check the install:

```sh
agentws version   # agentws v0.1.0-alpha.1 (commit abc1234)
```

### Mac app

Each release also has `agentws_<version>.dmg`. It is the native Mac app, with the matching `agentws` builds in it. Open it and drag `agentws.app` to Applications. The app is ad-hoc signed and not notarized, so macOS blocks its first launch. Remove the block one time with this command:

```sh
xattr -dr com.apple.quarantine /Applications/agentws.app
```

Or open the app, then go to System Settings › Privacy & Security › Open Anyway.

## Upgrade

`agentws version` prints the installed version. At most one time each day, it also checks GitHub for a newer release. To stop this check, set `AGENTWS_NO_UPDATE_CHECK=1`. `agentws` never updates itself. To upgrade, run the install script again. Then stop the old daemon, so that the new binary starts its own daemon:

```sh
curl -fsSL https://raw.githubusercontent.com/giovaniif/agent-workspace/main/scripts/install.sh | sh
agentws daemon stop
agentws version
agentws
```

## What it does

- **Sessions side by side.** Claude Code and Codex sessions run next to each other. They are grouped by task, with the name of the issue or PR they work on. Each session shows its state (running, waiting, done), model, effort and the context that is left.
- **Notifications.** You get a notification when an agent needs permission, waits for you or finishes. This is the same for all harnesses.
- **Limits.** The Claude 5h/7d windows and the Codex limits are in one bar. Before you start a session on a quota that is almost used, you get a warning.
- **Worktrees handled for you.** The agent creates worktrees. `agentws` attaches each worktree to the session that made it. When its PR merges and it has no uncommitted changes, `agentws` removes it automatically. If it has uncommitted changes, `agentws` backs them up and asks you first. `agentws` never deletes branches.
- **Ready-to-work worktrees.** A `[setup]` table in the repo's `.agentws.toml` prepares each worktree. It copies env templates, links caches, runs commands and shares `node_modules` through APFS clones. `agentws setup-worktree <path>` applies it.
- **Local review.** See the diff of the last agent turn, the uncommitted changes or the whole branch. Comment on lines, and send all the comments to the agent as one prompt.
- **Shell and nvim with one key.** Both open in the correct worktree. Diffs open in nvim through diffview.
- **Single repo or multi-repo.** Use it on a single repo, or on a folder that holds several service repos.

## Screens

| Review | Worktrees and disk |
|---|---|
| ![Review pane](docs/images/review.png) | ![Worktrees](docs/images/worktrees.png) |

![New session](docs/images/new-session.png)

## How it works

- One Go binary acts as the daemon, the TUI, the CLI, and the hook handler.
- Agents run in panes on a separate tmux server (`tmux -L agentws`). Your own tmux setup does not change. Sessions continue to run when you close the terminal. To go back to the sidebar, press `ctrl+\` in an agent pane.
- Claude Code and Codex hooks send state to the daemon through a local socket. The hook handler exits in less than 20 ms, so agents never wait for it.
- `agentws` calls git, gh and tmux as command-line tools. It does not reimplement them.

The details are in [ARCHITECTURE.md](ARCHITECTURE.md). The full scope is in [FEATURES.md](FEATURES.md).

## Getting started

Run `agentws`. The first time, a setup screen opens on top of the sidebar:

1. **Agents.** Select Claude Code, Codex or both (`↑/↓`, `space`, `⏎`). Each agent shows if its hooks are already set up.
2. **Claude Code.** The screen shows these items:
   - The file that it changes (`~/.claude/settings.json`, or `$CLAUDE_CONFIG_DIR`).
   - What it adds: the agentws hooks and a status-line wrapper that still runs your status line.
   - Where the backup goes.
   - How to undo it (`agentws setup claude --remove`).

   `⏎` installs, `s` skips.
3. **Codex.** The same for `~/.codex/hooks.json` (or `$CODEX_HOME`). Codex then needs a one-time trust step. Start `codex` and accept the review prompt for the new hooks, or trust them in `/hooks`.
4. **Neovim (optional).** This step applies if `nvim` is on `PATH` and no part of your nvim config configures the plugin yet.
   - The screen shows the file that it will write: `~/.config/nvim/plugin/agentws.lua`. This file loads the plugin that the install script put in `${XDG_DATA_HOME:-~/.local/share}/agentws/nvim`.
   - `⏎` writes it, `s` skips.
   - It does not edit your own files (init.lua, init.vim, lazy.nvim specs). `agentws setup nvim --remove` deletes the file.
   - Without nvim, the step tells how to install it (`brew install neovim`), and `⏎` continues. All keys except `e` and `o` work without nvim.
5. **Done.** Press `n` to start your first session.

At each step, `esc` skips the remaining steps. After you finish or skip, the screen does not open again automatically. It never opens when a harness is already set up and nvim has nothing more to configure. To open it again, press `S` in the sidebar or run `agentws setup`. `agentws setup claude`, `agentws setup codex` and `agentws setup nvim` do the same installs without the screen.

## Requirements

- macOS or Linux, tmux, git and the GitHub CLI (`gh`, signed in).
- Claude Code, the Codex CLI or both.
- For the nvim integration: Neovim 0.10+. For `:AgentwsDiff`, also [diffview.nvim](https://github.com/sindrets/diffview.nvim).

## Build and test

You need Go (the version in `go.mod`) and `golangci-lint` v2. For `make mutate`, you also need `gremlins`.

```sh
make build             # ./bin/agentws
./bin/agentws version
./bin/agentws debug seed 3 && ./bin/agentws   # the sidebar with 3 fake sessions
./bin/agentws workspace add ~/src/api && ./bin/agentws new "fix the flaky test"   # or press n in the TUI
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

1. Put the plugin dir on the runtimepath, with your plugin manager or with `vim.opt.rtp:prepend('<dir>')`. The dir is `${XDG_DATA_HOME:-~/.local/share}/agentws/nvim` from a release, or the repo's `nvim/` in a checkout.
2. Call `require('agentws').setup({})`.

The plugin needs `agentws` on `PATH`. It works in the nvim that the `e` key starts, which has `AGENTWS_SESSION` set.

- `:AgentwsDiff [last_turn|uncommitted|branch]` opens a review scope of the session in diffview. The default is the scope that the review pane has open.
- `:'<,'>AgentwsComment [text]` adds the selected lines as a draft review comment.
- `setup{bin = 'agentws', tmux = 'tmux', navigate = true}`. With `navigate = false`, the plugin does not change `C-h/j/k/l` in nvim.

## Mouse

Click a session to select it. Click it again to go to its agent pane. You can click these items:

- Key hints and buttons at the bottom of each screen.
- Picker choices, dialog fields and worktree rows.
- Review files, and scope and worktree chips.
- Diff lines. Drag over lines to select a range, then press `c` to comment.

The wheel moves through lists. In tmux, a click on a pane focuses it, and a drag on a border resizes. The wheel scrolls the history of an agent (`q` stops this). To select text with the terminal's own selection, hold Shift while you drag (Option in iTerm2 and Terminal.app). To turn off all mouse support:

```toml
# $AGENTWS_HOME/config.toml
[ui]
mouse = false
```

## Remote app

`agentws serve` serves a phone app (a PWA) for the sessions on this machine ([ADR 0046](docs/adr/0046-remote-app.md)). The app shows which session needs you, a chat view of each session, and new sessions. `agentws remote pair` prints a QR code that pairs a phone. You select how the phone gets to the machine. `agentws` only gives HTTPS. A PWA needs a certificate that the phone trusts. Thus `serve` gives plain HTTP only on loopback, and refuses it on all other addresses.

To keep it running, use `agentws setup serve`. It takes the same flags as `serve` (`--addr`, `--cert`, `--key`, `--self-signed`, `--url`):

```sh
agentws setup serve --addr 0.0.0.0:7420 --cert ~/machine.crt --key ~/machine.key
agentws setup serve --remove
```

On Linux, it writes the systemd user unit `~/.config/systemd/user/agentws-serve.service`. On macOS, it writes the launchd agent `~/Library/LaunchAgents/dev.agentws.serve.plist`. Each one:

- starts at login with the `PATH` and `AGENTWS_HOME` of the shell that ran the command
- restarts `serve` if it exits
- logs to `$AGENTWS_HOME/serve.log`.

If you run it again with the same arguments, nothing changes. With different arguments, it backs up the old file as `.bak` and writes it again. `--remove` stops the service and deletes the file. The unit holds the path of the binary, so run the command again after you move `agentws`.

A systemd user service stops when your last login session ends, unless lingering is on. On a Linux machine that you reach only through SSH, run `loginctl enable-linger $USER` yourself one time. `setup serve` does not do it.

`agentws setup daemon [--remove | --check]` does the same for the daemon (`agentws-daemon.service` or `dev.agentws.daemon`). It uses the `PATH` of your login shell, not the `PATH` of the caller. Thus sessions get a full `PATH`, also when you run it through a bare `ssh -T`. On Linux, if lingering is off, it shows a warning and prints the `sudo loginctl enable-linger` command. `--check` prints `{"installed","running","linger"}` as JSON.

**WARNING: Do not open `serve` to the internet, unless a layer in front of it adds its own authentication.** Pairing protects the app from strangers on a network that you already trust. It does not protect it from the open internet: each person who can get to the port can try pairing codes and probe the API. Use one of the setups below.

### Tailscale

Both machines are on one tailnet, and nothing is open to the internet. Get a certificate for the name of the machine, and let `serve` use it:

```sh
sudo tailscale cert machine.tailnet-name.ts.net
agentws setup serve --addr 0.0.0.0:7420 --cert machine.tailnet-name.ts.net.crt --key machine.tailnet-name.ts.net.key
agentws remote pair --url https://machine.tailnet-name.ts.net:7420
```

1. Make the certificate and key readable by your user.
2. Before they expire (about every 90 days), run `tailscale cert` again.
3. Restart with `systemctl --user restart agentws-serve`.

To use port 443 and renew without a change to `serve`, run a reverse proxy on the machine (see below). Let the proxy use the same certificate.

### LAN or VPN, behind Caddy or nginx

Keep `serve` on loopback. Let a proxy on the same machine terminate TLS with a Let's Encrypt certificate. The proxy must be reachable only from your LAN or VPN. A DNS-01 challenge issues the certificate for a name that points to a private address. Thus no port is open to the internet.

```sh
agentws setup serve
```

```caddyfile
agent.example.com {
    reverse_proxy 127.0.0.1:7420
}
```

With nginx, proxy to `http://127.0.0.1:7420`. Forward the WebSocket upgrade for `/api/v1/stream` (`proxy_http_version 1.1; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade"`).

If the proxy cannot get to the loopback of the host (for example, a proxy in a container), it connects through TLS:

1. Start `serve` with `--self-signed --addr 0.0.0.0:7420`.
2. Set the proxy to trust that certificate.

Pair with `agentws remote pair --url https://agent.example.com`.

### Cloudflare Tunnel

A tunnel needs no open port. Point `cloudflared` at the loopback `serve`. Put a Cloudflare Access application in front of the hostname. Thus a login occurs at Cloudflare before a request gets to the machine:

```yaml
ingress:
  - hostname: agent.example.com
    service: http://127.0.0.1:7420
  - service: http_status:404
```

Create the Access application for `agent.example.com` with a policy that allows only you. The installed app must also go through Access. Access sessions end, so do one of these:

- Use a long session duration.
- Add a service token policy for the `/api/v1/*` path of the app, and keep the interactive login for the other paths.

Without Access (or a different layer that authenticates), the tunnel is a public endpoint, and the warning above applies.

## Codex setup

`agentws setup codex` adds the hooks that Codex needs to send state. It also prints the one-time trust step that Codex requires. `agentws setup codex --remove` removes the hooks. It backs up an existing `hooks.json` first, and does not change your other hooks.

## Contributing

The build is test-driven, and CI enforces it. Read [AGENTS.md](AGENTS.md) before you open a PR. It covers the layer rules, tests first, no low-value tests or comments, and one PR per issue. Write docs in the style of [docs/style.md](docs/style.md).
