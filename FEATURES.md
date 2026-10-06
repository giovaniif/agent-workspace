# Agent workspace: feature definition (v1)

Status: approved 2026-09-30. `agentws` is a placeholder name.

One terminal tool for running Claude Code, Codex, and Oh My Pi (`omp`) sessions in parallel, each in its own worktree, with a PR-style review pane. It replaces the current tmux setup.

## Decisions

- **Client:** a TUI first. A native macOS client can come later on the same daemon.
- **Terminals:** tmux runs in the background on its own server socket. Our TUI draws the sidebar and review pane, and swaps agent panes into the main area. We don't use the tmux status line or copy a sidebar pane into each window.
- **Harnesses:** Claude Code, Codex, and Oh My Pi (`omp`). Claude and Codex report usage limits. omp reports model and effort only.
- **Worktree cleanup:** a worktree is removed automatically when its PR is merged, it has no uncommitted changes, and no session uses it. If it has uncommitted changes, they are backed up and you are asked first.

## Evidence (last 57 days, as of 2026-09-29)

- You run 2–4 sessions at once (median) and up to 10. Sessions stay open for days: median 1.5h, p90 29h.
- Getting PRs merged is the main loop: babysit-pr ran 190 times, 34% of prompts are about PRs or CI, and agents ran `gh pr view` 2,213 times.
- Agents created 245 worktrees and removed only 61. Before cleanup that was 300 worktrees and 451G `du`, with the disk 99% full. A one-off cleanup removed 272 of them.
- You hit the Claude session limit on 5 days, always in the afternoon, with several sessions blocked at once.
- 95% of work is in one orchestration root holding ~14 service repos, and tasks often span two of them.
- Frequent reorienting: 46 `/resume`, 27 `/btw`, "which branch is on port 8081?".
- Existing tools: T3 Code and Conductor have a GUI review loop. herdr, Claude Squad and cmux are terminal-only with no review loop. None of them combines usage limits across harnesses or cleans up worktrees safely.

## Model

- **Workspace:** a folder the tool manages. It is either a single repo, or an orchestration root that holds several service repos. Repos are discovered automatically: any child folder with its own `.git` directory, symlinks included. A child whose `.git` is a file is a worktree, not a repo.
- **Task:** a work item such as a Linear issue, a PR or free text. Sessions are grouped under it.
- **Session:** one agent (Claude, Codex, or omp) running in one terminal. Its working directory is the workspace root, so the root `AGENTS.md`/`CLAUDE.md`, skills and memory apply. A session can have any number of worktrees.
- **Worktree:** one repo at one branch, with its own PR, checks, port, and cleanup. A session can hold several worktrees in the same repo, one per subtask.

## Architecture

- **Daemon:** owns sessions, worktrees, harness adapters, PR polling, usage data and notifications, and persists state to disk.
- **Harness adapters:** Claude Code, Codex, and omp. Their hook events map to one state model: `running`, `waiting`, `permission`, `done`, `idle`. Claude and Codex also report context left and usage limits. omp reports model and effort from its hook.
- **Terminal host:** a separate tmux server (`tmux -L agentws`) with one pane per agent and one per shell. Only the adapter talks to tmux, so it can be replaced later.
- **TUI:** connects to the daemon over a local socket. Closing it leaves the sessions running. `q` and ctrl+c always close it, even when the daemon is gone. When the daemon restarts, the sidebar reconnects on its own (it never starts one); after an upgrade it says so and `r` restarts it on the new binary.

## P0 (v1)

**Sessions**
- The sidebar has vertical tabs grouped by task, showing each session's state glyph, harness, model, effort and context left. A session row expands to show its worktrees, each with repo, subtask, PR and check status (for example `api:shares #42 ✓`).
- The session header shows every PR the session owns, each with its check status.
- Sessions are named automatically from the PR, then the Linear issue, then the task. A text task is named by a 2-4 word summary of its first prompt. `R` renames and pins a name, `A` unpins it. The name is the work item, never the agent's current activity.
- Keyboard-first navigation: jump to a session by number, next waiting session, last session.
- The new-session dialog takes only the workspace, work item (Linear issue, PR or text), harness, model and effort. The workspace defaults to the last one used.
  - You never pick repos or create worktrees yourself. Creating them is the agent's job; the tool detects each one and attaches it to the session.
  - Single-repo workspace: the session starts in one fresh worktree branched from `origin/main`, with the repo's setup recipe already run.
  - Orchestration root: the session starts at the root, and the agent creates worktrees in the service repos it needs.
- Sessions survive TUI restarts, terminal crashes and SSH detach.
- An ended session that still has a worktree can be resumed (`u`): the harness picks its conversation back up in the same dir, with the same model and effort.

**Attention**
- Notifications for waiting, permission requests and done: macOS banner, optional sound, and per-session mute (`m`). No banner for the session you are looking at while the terminal is in front, and at most one per session every 10 s. The title names the session and where it runs (`fix login · api@42-retry`); the body says what it needs (`needs permission: Bash: npm test`, `asks: Should I keep the alias?`) or what it did (the first line of its last message and how long the turn took), and says so when it hit a usage limit or an API error. With `terminal-notifier` installed, a session keeps one banner (a newer one replaces it), clicking it brings the terminal to the front on that session (`agentws focus <id>`), and it is withdrawn once the session resumes or is focused.
- An unread marker until you look at the session. Waiting sessions sort to the top.
- Claude and Codex notify on the same events.

**Model, effort, limits**
- A global bar showing Claude 5h/7d windows and Codex limits, with the percentage used and the clock time each resets.
- A warning before starting a session when little quota is left.
- Change a session's model or effort with one key. Defaults are set per harness.

**Worktrees**
- Worktree location is set per workspace. An orchestration root defaults to `<root>/.worktrees/<repo>-<task>[-<subtask>]`, the convention existing skills and memory already reference. Single repos default to `~/.agentws/worktrees/<repo>/<task>[-<subtask>]`.
- Worktrees the agent creates are attached to the session that created them. Detection uses a hook that fires after `git worktree add` runs, plus polling `git worktree list`. Subagent worktrees (`.claude/worktrees/agent-*`) attach to the parent session.
- A setup recipe per repo, e.g. copy an env template and run `bun install`. Dependencies are shared through APFS clones or hardlinks, never a fresh install per worktree.
- Cleanup is decided per worktree, not per task or session. A session with 4 PRs frees each worktree as its own PR merges.
- When a PR is merged, clean worktrees not open in a shell, nvim or dev server are removed on their own. Uncommitted changes are saved as a patch plus a tarball of untracked files, then you're asked. Detached commits get a backup branch. Local branches are never deleted.
- A disk view with size per worktree, reclaimable total, and free space on the volume.
- Existing worktrees are adopted at first run.
- A ports view showing which dev server and port belongs to which worktree, with one-key kill.

**Review pane**
- A side pane like a PR review: file tree, unified or split diff, syntax highlighting, and a marker for files you've already viewed.
- Scope toggle: last agent turn / uncommitted / whole branch vs base.
- A worktree switcher: "all" or a single worktree. In "all", the file tree is grouped by repo, then subtask and PR.
- Review is local: diffs come from git in each worktree, not from GitHub.
- Line and range comments are collected into a draft review and sent to the session as a single prompt. Each comment carries its worktree and file path, so the agent edits the right checkout.
- Stage or revert per hunk.

**Terminal and nvim**
- One key opens a shell in the session's selected worktree (or the workspace root), as a split or a popup.
- One key opens the current file and line in nvim. A session can have a long-lived nvim in its own pane, controlled through `nvim --listen`.
- Diffs open in nvim with diffview, and comments written in nvim land in the same draft review.
- `C-h/j/k/l` moves between nvim splits and panes without the current tmux conflict.

## P1

- A PR board per session: checks with failing job names linked to their runs, review-bot comments since the last push, unresolved threads, merge readiness. It shows as `agentws pr <session> [--json]`, which babysit-pr reads instead of polling `gh` (see ADR 0024).
- A subagent tree per session under its sidebar row, from the harness's subagent hooks. Claude only today; stop and steer controls are not offered because neither harness exposes them per subagent (see ADR 0019).
- A Linear launcher: `L` takes one or more issue URLs and starts a worktree and session for each, up to `[launcher] max_parallel` (default 3) at once; the rest queue and start as sessions finish. See ADR 0031.
- When Claude quota is low, offer to start queued work in Codex.
- Mouse: click a session to select it and jump to its pane, click key hints and buttons, pick from lists, open review files, place the review cursor or drag a range, and scroll with the wheel. In tmux, click focuses a pane, dragging a border resizes, and the wheel scrolls agent history. Shift-drag (Option in some macOS terminals) keeps the terminal's own selection; `[ui] mouse = false` turns it all off (see ADR 0042).
- A first-run walkthrough: pick Claude, Codex or both, see each one's hook state and install it with a backup, follow Codex's trust step, and get the nvim snippet for the installed plugin. `S` or `agentws setup` reopens it (see ADR 0040).

## P2

- Native macOS client.
- A phone app: a PWA served by `agentws serve` that lists sessions, chats with them, starts new ones and gets push notifications, over whatever network path the user picks (see ADR 0046). Pushes are held while you type in an attached `agentws` tmux client (`[push] away_after`, default 2 minutes, `"0"` turns it off) and sent once if you step away with a session still needing you; a device whose app is open on screen gets none.
- Comparing two agents' attempts at the same task side by side.
- Attaching to a remote daemon over SSH.

## Not in v1

- A built-in editor (nvim does that), cloud sandboxes, multiplayer, Docker isolation, Windows.

## Still open

Stack and architecture are decided in [ARCHITECTURE.md](ARCHITECTURE.md).


- Product name (`agentws` is a placeholder).
- Whether the existing `~/.tmux/agent-hooks` scripts are ported or replaced by the adapters.
