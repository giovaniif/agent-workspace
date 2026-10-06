# agentws for macOS: UX design

Status: draft, 2026-10-06. Mockups: the "agentws for macOS" design canvas. Architecture: [ADR 0049](adr/0049-macos-app.md).

## Goal

A native macOS app (Swift) that does everything the TUI does, on the same daemon, for a daemon on this Mac or on another machine reached over SSH. The TUI keeps working; a session can move between the TUI, the phone app and the Mac app at any time.

Not goals for v1: replacing tmux as the terminal host, a built-in editor, iOS.

## Principles

1. **Attention first.** The sidebar, menu bar, Dock badge and banners all answer one question: which session needs me, and for what. Same rule as `domain.Sidebar`: sessions that need you (permission or waiting) first, then the rest in the daemon's order.
2. **The terminal is the session.** The agent's real pane is the centre of the window. Native chrome surrounds it. There is no chat view: transcripts stay a phone feature.
3. **Keyboard parity with the TUI.** Every TUI key has a Mac shortcut (table below). Mouse is a full second path, not an afterthought.
4. **The daemon decides, the app draws.** Names, banners, quotas, cleanup decisions, card contents come from `domain` via the daemon, as the phone app does. The app never re-derives them.
5. **Native, quiet, Latte.** Standard macOS window, sidebar, toolbar, sheets and settings. Catppuccin Latte colours for continuity with the TUI and the phone app; system font for UI, a mono font for code and terminals. Dark mode uses Mocha.

## Window structure (screen 1)

Three columns in one `NavigationSplitView`-style window:

| Column | Width | Contents |
|---|---|---|
| Sidebar | 260–340 | Filter (⌘K), one flat list of sessions (no task headers), then a collapsed "Ended" group (resume) and a footer with totals and reclaimable disk |
| Main | flexible | Session header strip, then the active view: Terminal, Review, Shell or nvim |
| Inspector | 300–360, toggled ⌥⌘I | The PR board only. What the agent waits on and its recent actions are left to the harness in the terminal. |

**Toolbar:** sidebar toggle; session name and `repo@branch +N`; view switcher (Terminal · Review · Shell · nvim); quota meters (Claude 5h, 7d, Codex: % used, reset time, orange under 20% left, dimmed when stale); server chip; New; inspector toggle.

**Sidebar order:** `domain.Sidebar`'s rule over one flat list: sessions that need you first, then the rest in the daemon's order. ⌘1–9 follow that order.

**Sidebar row:** state glyph, name (bold when unread), harness badge (CC/CX), then `repo@branch +N`, then `model · effort · ctx %`, ⌘N shortcut. Expands to worktree rows: `repo:subtask #PR ✓ :port`. Muted sessions show a bell-slash. Context menu: Rename, Unpin name, Mute, Switch model, Switch effort, End, Show worktrees, Copy branch.

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

Opened with ⌘R or the view switcher. The sidebar collapses to a rail of numbered state badges (as the TUI does) and comes back with ⌃⌘S.

- Scope: Last turn · Uncommitted · Branch vs main (`[` and `]`).
- Worktree menu: All, then each worktree. In All, the file tree groups by worktree with its PR.
- Unified or split diff, syntax highlighted, hunk headers with Stage hunk and Revert… (confirmation).
- Viewed checkbox per file, `V` toggles; viewed files are ticked in the tree.
- Comments: click a line number or press `C`; shift-click or drag for a range. The comment pops inline under the line.
- Draft review panel on the right: every comment with `worktree · file:line`, an overall note, and Send review to session (⇧⌘↩). It says when the session is busy and the review will queue.
- Open in nvim (⌘E) opens the diff's top visible line in the session's nvim and switches to the nvim view.

## New session (screen 3)

A sheet (⌘N), fields in this order: Server, Workspace (last used preselected; shows "orchestration root · 14 repos" or "single repo"), Work item (Linear issue, PR link or text, resolved live into a card that names the branch and any existing worktree), Agent (Claude Code · Codex), Model, Effort. No first-prompt field: you type the first prompt in the session's terminal. A quota warning appears when the chosen harness is low, with one button to switch harness. ⌘↩ starts, Esc cancels. No repo or worktree pickers: the agent creates worktrees.

The Linear launcher (TUI `L`) is a second tab in the same sheet: paste several issue URLs, see the queue and `max_parallel`.

## Attention (screen 4)

- **Permission card.** When a session is in `permission`, a card floats over the bottom of its terminal with the parsed request and one button per choice (keys 1–3, Esc). "Show terminal" hides the card. If the dialog cannot be parsed, no card: the terminal already shows it. Uses `session.prompt` / `session.answer`, so a stale card cannot answer the wrong dialog.
- **Notifications.** `UNUserNotificationCenter` banners with the daemon's title and body (`fix login · api@42-retry` / `needs permission: Bash: npm test`). One per session, replaced by newer ones, withdrawn when the session resumes or is focused. Actions: Allow / Open for permission, Reply… (inline text, `session.send`) / Open for waiting. No banner for the session in view while the app is frontmost.
- **Menu bar extra.** Icon with counts (needs you · done unread). The menu lists Needs you (permission rows with Allow / Always / Open), Done · unread, Working, the quota meters, New session…, Open agentws. Works with the main window closed.
- **Dock badge.** Number of sessions that need you.

## Worktrees, disk and ports (screen 5)

A separate window (⇧⌘W) with three tabs: Worktrees, Ports, Recently cleaned.

- Header tiles: volume free, worktree total, reclaimable (with `+` while sizes are measured), shared deps store, auto-cleanup schedule.
- Table: worktree, session, PR, state (Open, Merged · clean, Merged · dirty, Detached, No PR), size (`…` while measuring), ports, what cleanup will do (removes at 14:30, back up then ask, backup branch first, kept: dev server running, in use).
- The action bar for the selected row says exactly what will happen before it happens: Go to session, Open shell, Kill dev servers, Remove / Back up and remove… (always confirmed).

## Servers and settings (screen 6)

Settings tabs (screens 7a–7f):

- **General:** open at login, server to connect at launch, menu bar and Dock (what the icon and badge count), ended sessions shown, confirm on end, show worktrees and subagents, the `agentws` CLI on PATH, updates.
- **Servers:** below.
- **Workspaces** (per server): repos discovered, worktree location, auto-cleanup and its interval, setup recipes per repo (`.agentws.toml`), launcher `max_parallel`.
- **Agents** (per server): Claude Code and Codex install, hooks (with backup, removable), status line, Codex trust, default model and effort; quota warning threshold, Codex fallback offer, stale-limit age; nvim and its plugin.
- **Notifications:** banner and sound per event (permission, waiting, done, limit or API error), skip the session in view, muted sessions, and phones: the hold-while-present window (`[push] away_after`), paired devices with revoke, pair a phone.
- **Appearance:** Latte, Mocha or match system, accent, `[theme]` overrides from `config.toml`, terminal font, size, line height and cursor with a live preview, sidebar density, default diff layout.
- **Shortcuts:** every action with its key recorder and the TUI key beside it, restore defaults, and the terminal pass-through switch.

**Servers:** a list of daemons, "This Mac" plus any number of SSH hosts. One window shows one server at a time; the server chip in the toolbar switches. Each server shows a status checklist: SSH reachable (latency), daemon reached and build matches, terminals, notifications, gh signed in, each with a fix-it button where there is one ("Update server", "Run gh auth login", "Remove old bridge"). SSH uses the user's `ssh` binary, `~/.ssh/config` and agent; the app stores no keys.

## First run (screen 8)

A six-step window on first launch. The same steps run again from Settings › Servers › +.

1. **Welcome.**
2. **Where agents run:** This Mac, or a server over SSH.
3. **Server:** hosts listed from `~/.ssh/config`, or `user@host` typed in. The app tests with `ssh -o BatchMode=yes`, so it needs key or agent login (Tailscale SSH works too). It never asks for or stores a password. It reports the OS, the architecture and whether `agentws` is installed and at which build.
4. **Set up the server,** all in the user's account with no sudo:
   - install the `agentws` build matching the app into `~/.local/bin`;
   - run the daemon as a `systemd --user` service (a launchd agent on a Mac server) with the login shell's PATH, so it outlives the SSH connection. A daemon started over a bare `ssh -T` would hand every agent pane a login-less PATH;
   - check `git`, `tmux` and `gh` (and that `gh` is signed in);
   - install the Claude Code and Codex hooks through the existing walkthrough methods (`onboarding.status` and `onboarding.install`), with a backup. Codex is optional.
5. **Workspace:** a path on the server, typed or browsed over SSH. The app shows whether it is a single repo or an orchestration root, and how many existing worktrees will be adopted (`workspace.add`).
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

Inside a focused terminal every key goes to the pane except ⌘ shortcuts, so agents and nvim keep `C-h/j/k/l` and Escape.

## Architecture

Decided in [ADR 0049](adr/0049-macos-app.md): one stdio transport (`agentws rpc`, locally or over `ssh -T`), derived values from Go through `view.subscribe`, terminals over tmux control mode (after a spike), highlighting tokens from the Go side, `client.viewing` for presence, and server setup that installs the app's own `agentws` build.

## Delivery order

The phases in ADR 0049, tracked as issues in the `macOS app` milestone.

## Open questions

- One window per server, or several servers in one sidebar?
- Product name, still a placeholder.
