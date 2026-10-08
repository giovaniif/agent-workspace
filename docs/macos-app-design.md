# agentws for macOS: UX design

Status: draft, 2026-10-06. Mockups: the "agentws for macOS" design canvas. Architecture: [ADR 0049](adr/0049-macos-app.md).

## Goal

A native macOS app (Swift) that does all that the TUI does, on the same daemon. The daemon can be on this Mac, or on a different machine that the app reaches through SSH. The TUI continues to work. At any time, a session can move between the TUI, the phone app and the Mac app.

Not goals for v1: replacing tmux as the terminal host, a built-in editor, iOS.

## Principles

1. **Attention first.** The sidebar, menu bar, Dock badge and banners all answer one question: which session needs me, and for what? The rule is the same as `domain.Sidebar`: first the sessions that need you (permission or waiting), then the others in the order of the daemon.
2. **The terminal is the session.** The real pane of the agent is the centre of the window. Native chrome is around it. There is no chat view: transcripts are only a phone feature.
3. **Keyboard parity with the TUI.** Each TUI key has a Mac shortcut (see the table below). The mouse is a full second path, not an afterthought.
4. **The daemon decides, the app draws.** Names, banners, quotas, cleanup decisions and card contents come from `domain` through the daemon, as for the phone app. The app never calculates them again.
5. **Native, quiet, Latte.** A standard macOS window, sidebar, toolbar, sheets and settings. Catppuccin Latte colours, the same as the TUI and the phone app. The system font for UI, and a mono font for code and terminals. Dark mode uses Mocha.

## Window structure (screen 1)

Three columns in one `NavigationSplitView`-style window:

| Column | Width | Contents |
|---|---|---|
| Sidebar | 260–340 | Filter (⌘K), one flat list of sessions (no task headers), then a collapsed "Ended" group (resume) and a footer with totals and reclaimable disk |
| Main | flexible | Session header strip, then the active view: Terminal, Review, Shell or nvim |
| Inspector | 300–360, toggled ⌥⌘I | The PR board only. The harness in the terminal shows what the agent waits for and its recent actions. |

**Toolbar:**

- sidebar toggle
- session name and `repo@branch +N`
- view switcher (Terminal · Review · Shell · nvim)
- quota meters (Claude 5h, 7d, Codex): % used and reset time. A meter is orange under 20% left, and dimmed when stale.
- server chip
- New
- inspector toggle.

**Sidebar order:** the rule of `domain.Sidebar` on one flat list: first the sessions that need you, then the others in the order of the daemon. ⌘1–9 use that order.

**Sidebar row:** state glyph, name (bold when unread), harness badge (CC/CX), then `repo@branch +N`, then `model · effort · ctx %`, and the ⌘N shortcut. The row expands to worktree rows: `repo:subtask #PR ✓ :port`. Muted sessions show a bell-slash. Context menu: Rename, Unpin name, Mute, Switch model, Switch effort, End, Show worktrees, Copy branch.

**State glyphs** (shape and colour, never colour alone):

| State | Glyph | Colour |
|---|---|---|
| permission | filled diamond | red |
| waiting | filled dot | peach |
| running | spinning ring | blue |
| done | filled dot | green |
| idle | hollow ring | grey |

**Header strip:** state chip with time in state, one chip per PR with check status, model and context left.

**Status line:** tmux pane id and the main shortcuts.

## Review (screen 2)

Open it with ⌘R or the view switcher. The sidebar collapses to a rail of numbered state badges, as in the TUI. ⌃⌘S shows the sidebar again.

- Scope: Last turn · Uncommitted · Branch vs main (`[` and `]`).
- Worktree menu: All, then each worktree. In All, the file tree groups by worktree with its PR.
- Unified or split diff, syntax highlighted, hunk headers with Stage hunk and Revert… (confirmation).
- A Viewed checkbox for each file. `V` toggles it. The tree shows a tick on viewed files.
- Comments: click a line number or press `C`. Shift-click or drag for a range. The comment opens inline under the line.
- Draft review panel on the right: each comment with `worktree · file:line`, an overall note, and Send review to session (⇧⌘↩). If the session is busy, the panel tells you that the review will wait in the queue.
- Open in nvim (⌘E) opens the top visible line of the diff in the nvim of the session, and shows the nvim view.

## New session (screen 3)

A sheet (⌘N) with these fields, in this order:

1. Server.
2. Workspace. The last used one is preselected. It shows "orchestration root · 14 repos" or "single repo".
3. Work item: a Linear issue, a PR link or text. The sheet resolves it live into a card that names the branch and any existing worktree.
4. Agent (Claude Code · Codex).
5. Model.
6. Effort.

There is no first-prompt field: you type the first prompt in the terminal of the session. When the selected harness is low on quota, a warning appears, with one button to switch harness. ⌘↩ starts, Esc cancels. There are no repo or worktree pickers: the agent creates worktrees.

The Linear launcher (TUI `L`) is a second tab in the same sheet. Paste several issue URLs, and see the queue and `max_parallel`.

## Attention (screen 4)

- **Permission card.** When a session is in `permission`, a card floats over the bottom of its terminal. It shows the parsed request and one button for each choice (keys 1–3, Esc). "Show terminal" hides the card. If the app cannot parse the dialog, there is no card: the terminal already shows the dialog. The card uses `session.prompt` / `session.answer`, so a stale card cannot answer the wrong dialog.
- **Notifications.** `UNUserNotificationCenter` banners with the title and body of the daemon (`fix login · api@42-retry` / `needs permission: Bash: npm test`).
  - There is one banner for each session. A newer banner replaces it. It goes away when the session resumes or gets focus.
  - Actions: Allow / Open for permission. Reply… (inline text, `session.send`) / Open for waiting.
  - While the app is frontmost, there is no banner for the session in view.
- **Menu bar extra.** An icon with counts (needs you · done unread). The menu lists Needs you (permission rows with Allow / Always / Open), Done · unread, Working, the quota meters, New session… and Open agentws. It works when the main window is closed.
- **Dock badge.** Number of sessions that need you.

## Worktrees, disk and ports (screen 5)

A separate window (⇧⌘W) with three tabs: Worktrees, Ports, Recently cleaned.

- Header tiles: volume free, worktree total, reclaimable (with `+` while the daemon measures sizes), shared deps store, auto-cleanup schedule.
- Table: worktree, session, PR, state (Open, Merged · clean, Merged · dirty, Detached, No PR), size (`…` while measuring), ports, and what cleanup will do (removes at 14:30, back up then ask, backup branch first, kept: dev server running, in use).
- Before an action occurs, the action bar for the selected row tells exactly what it will do. The actions are Go to session, Open shell, Kill dev servers, and Remove / Back up and remove… (always with a confirmation).

## Servers and settings (screen 6)

Settings tabs (screens 7a–7f):

- **General:** open at login, server to connect at launch, menu bar and Dock (what the icon and badge count), ended sessions shown, confirm on end, show worktrees and subagents, the `agentws` CLI on PATH, updates.
- **Servers:** below.
- **Workspaces** (per server): repos discovered, worktree location, auto-cleanup and its interval, setup recipes per repo (`.agentws.toml`), launcher `max_parallel`.
- **Agents** (per server): Claude Code and Codex install, hooks (with backup, removable), status line, Codex trust, default model and effort; quota warning threshold, Codex fallback offer, stale-limit age; nvim and its plugin.
- **Notifications:** banner and sound per event (permission, waiting, done, limit or API error), skip the session in view, muted sessions, and phones: the hold-while-present window (`[push] away_after`), paired devices with revoke, pair a phone.
- **Appearance:** Latte, Mocha or match system, accent, `[theme]` overrides from `config.toml`, terminal font, size, line height and cursor with a live preview, sidebar density, default diff layout.
- **Shortcuts:** every action with its key recorder and the TUI key beside it, restore defaults, and the terminal pass-through switch.

**Servers:** a list of daemons: "This Mac" and any number of SSH hosts. One window shows one server at a time. The server chip in the toolbar switches between them. Each server shows a status checklist: SSH reachable (latency), daemon reached and build matches, terminals, notifications, gh signed in. Where a fix is available, an item has a fix-it button ("Update server", "Run gh auth login", "Remove old bridge"). SSH uses the `ssh` binary, `~/.ssh/config` and agent of the user. The app stores no keys.

## First run (screen 8)

A six-step window on first launch. The same steps run again from Settings › Servers › +.

1. **Welcome.**
2. **Where agents run:** This Mac, or a server over SSH.
3. **Server:** the hosts from `~/.ssh/config`, or a `user@host` that you type. The app tests with `ssh -o BatchMode=yes`, so it needs key or agent login (Tailscale SSH also works). It never asks for a password or stores one. It reports the OS, the architecture, and if `agentws` is installed, at which build.
4. **Set up the server,** all in the user's account with no sudo:
   - install the `agentws` build matching the app into `~/.local/bin`;
   - run the daemon as a `systemd --user` service (a launchd agent on a Mac server) with the PATH of the login shell, so that it continues after the SSH connection ends. A daemon that starts through a bare `ssh -T` gives each agent pane a PATH without login settings;
   - check `git`, `tmux` and `gh` (and that `gh` is signed in);
   - install the Claude Code and Codex hooks through the existing walkthrough methods (`onboarding.status` and `onboarding.install`), with a backup. Codex is optional.
5. **Workspace:** a path on the server. Type it, or browse for it through SSH. The app shows if it is a single repo or an orchestration root, and how many existing worktrees it will adopt (`workspace.add`).
6. **Done:** a summary, the notification permission prompt, and a reminder that `ssh -t host agentws` opens the TUI on the same sessions.

For "This Mac", steps 3–4 become: link the bundled `agentws` into `/usr/local/bin`, start the daemon as a launchd agent, check the tools, install the hooks.

## Keyboard map

| Action | TUI | Mac |
|---|---|---|
| Jump to session N | `1`–`9` | ⌘1–⌘9 |
| Next waiting session | `space` | ⌃Space |
| Last session | — | ⌘[ |
| Filter sessions | — | ⌘K |
| New session | `n` | ⌘N |
| Linear launcher | `L` | ⇧⌘N |
| Review | `r` | ⌘R |
| Shell in worktree (split / popup) | `t` / `T` | ⌘T / ⇧⌘T |
| nvim at file | `e` | ⌘E |
| Rename and pin / unpin | `R` / `A` | ⌘⇧R / from the menu |
| Model / effort | `M` / `E` | ⌃⌘M / ⌃⌘E |
| Mute | `m` | ⌥⌘M |
| End session | `x` `y` | ⌘⌫ (confirm) |
| Resume ended | `u` | ⇧⌘U |
| Worktrees and disk | `w` | ⇧⌘W |
| Kill session's dev servers | `K` | ⌥⌘K (confirm) |
| Inspector | — | ⌥⌘I |
| Back to sidebar from terminal | `ctrl+\` | ⌘0 |

In a focused terminal, all keys except ⌘ shortcuts go to the pane. Thus agents and nvim keep `C-h/j/k/l` and Escape.

## Architecture

[ADR 0049](adr/0049-macos-app.md) gives these decisions:

- one stdio transport (`agentws rpc`, locally or through `ssh -T`)
- derived values from Go through `view.subscribe`
- terminals through tmux control mode (after a spike)
- highlighting tokens from the Go side
- `client.viewing` for presence
- a server setup that installs the `agentws` build of the app.

## Delivery order

The phases in ADR 0049, tracked as issues in the `macOS app` milestone.

## Open questions

- One window per server, or several servers in one sidebar?
- Product name, still a placeholder.
