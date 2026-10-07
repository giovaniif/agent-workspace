# Agent workspace: feature definition (v1)

Status: approved 2026-09-30. `agentws` is a placeholder name.

One terminal tool that runs Claude Code, Codex and Oh My Pi (`omp`) sessions in parallel. Each session works in its own worktree and has a PR-style review pane. The tool replaces the current tmux setup.

## Decisions

- **Client:** a TUI first. A native macOS client can come later on the same daemon.
- **Terminals:** tmux runs in the background on its own server socket. Our TUI draws the sidebar and the review pane, and swaps agent panes into the main area. We do not use the tmux status line. We do not copy a sidebar pane into each window.
- **Harnesses:** Claude Code, Codex, and Oh My Pi (`omp`). Claude and Codex report usage limits. omp reports model and effort only.
- **Worktree cleanup:** the tool removes a worktree automatically when these conditions are true:
  - Its PR is merged.
  - It has no uncommitted changes.
  - No session uses it.

  If it has uncommitted changes, the tool makes a backup of them and asks you first.

## Evidence (last 57 days, as of 2026-09-29)

- You run 2–4 sessions at once (median) and up to 10. Sessions stay open for days: median 1.5h, p90 29h.
- Getting PRs merged is the main loop: babysit-pr ran 190 times, 34% of prompts are about PRs or CI, and agents ran `gh pr view` 2,213 times.
- Agents created 245 worktrees and removed only 61. Before cleanup that was 300 worktrees and 451G `du`, with the disk 99% full. A one-off cleanup removed 272 of them.
- You hit the Claude session limit on 5 days, always in the afternoon, with several sessions blocked at once.
- 95% of work is in one orchestration root holding ~14 service repos, and tasks often span two of them.
- Frequent reorienting: 46 `/resume`, 27 `/btw`, "which branch is on port 8081?".
- Existing tools: T3 Code and Conductor have a GUI review loop. herdr, Claude Squad and cmux are terminal-only with no review loop. None of them combines usage limits across harnesses or cleans up worktrees safely.

## Model

- **Workspace:** a folder that the tool manages. It is a single repo, or an orchestration root that holds several service repos. The tool finds the repos automatically: each child folder with its own `.git` directory is a repo, symlinks included. A child whose `.git` is a file is a worktree, not a repo.
- **Task:** a work item, for example a Linear issue, a PR or free text. Sessions are grouped under it.
- **Session:** one agent (Claude, Codex or omp) that runs in one terminal. Its working directory is the workspace root. Thus the root `AGENTS.md`/`CLAUDE.md`, skills and memory apply. A session can have any number of worktrees.
- **Worktree:** one repo at one branch, with its own PR, checks, port, and cleanup. A session can hold several worktrees in the same repo, one per subtask.

## Architecture

- **Daemon:** owns sessions, worktrees, harness adapters, PR polling, usage data and notifications. It keeps state on disk.
- **Harness adapters:** Claude Code, Codex and omp. Their hook events map to one state model: `running`, `waiting`, `permission`, `done`, `idle`. Claude and Codex also report the context that is left and usage limits. omp reports model and effort from its hook.
- **Terminal host:** a separate tmux server (`tmux -L agentws`) with one pane for each agent and one for each shell. Only the adapter talks to tmux, so it is possible to replace tmux later.
- **TUI:** connects to the daemon through a local socket. When you close it, the sessions continue to run. `q` and ctrl+c always close it, also when the daemon is stopped. When the daemon restarts, the sidebar connects again automatically. It never starts a daemon. After an upgrade, it tells you, and `r` restarts the daemon on the new binary.

## P0 (v1)

**Sessions**
- The sidebar has vertical rows grouped by task. Each row shows the state glyph, harness, model, effort and context left of a session. A session row expands to show its worktrees. Each worktree shows its repo, subtask, PR and check status (for example `api:shares #42 ✓`).
- The session header shows every PR the session owns, each with its check status.
- The tool names sessions automatically from the PR, then the Linear issue, then the task. The name of a text task is a 2-4 word summary of its first prompt. `R` renames and pins a name, `A` unpins it. The name is the work item, never the current activity of the agent.
- Keyboard-first navigation: go to a session by number, to the next waiting session, or to the last session.
- The new-session dialog takes only the workspace, work item (Linear issue, PR or text), harness, model and effort. The default workspace is the last one used.
  - You never select repos or create worktrees yourself. The agent creates them. The tool finds each one and attaches it to the session.
  - Single-repo workspace: the session starts in one fresh worktree branched from `origin/main`, with the repo's setup recipe already run.
  - Orchestration root: the session starts at the root, and the agent creates worktrees in the service repos it needs.
- Sessions survive TUI restarts, terminal crashes and SSH detach.
- You can resume an ended session that still has a worktree (`u`). The harness continues its conversation in the same dir, with the same model and effort.

**Attention**
- Notifications for waiting, permission requests and done: a macOS banner, an optional sound, and a mute for each session (`m`).
  - There is no banner for the session that you view while the terminal is in front. There is a maximum of one banner for each session every 10 s.
  - The title gives the session and where it runs (`fix login · api@42-retry`).
  - The body tells what the session needs (`needs permission: Bash: npm test`, `asks: Should I keep the alias?`). Or it tells what the session did: the first line of its last message and the duration of the turn. It also tells you when the session got a usage limit or an API error.
  - With `terminal-notifier` installed, a session keeps one banner, and a newer banner replaces it. A click on the banner focuses the terminal on that session (`agentws focus <id>`). The banner disappears when the session resumes or gets focus.
- An unread marker stays until you view the session. Waiting sessions move to the top.
- Claude and Codex notify on the same events.

**Model, effort, limits**
- A global bar shows the Claude 5h/7d windows and the Codex limits. Each limit shows the percentage used and the clock time when it resets.
- Before you start a session with little quota left, you get a warning.
- Change the model or effort of a session with one key. Each harness has its own defaults.

**Worktrees**
- Each workspace has its own worktree location. For an orchestration root, the default is `<root>/.worktrees/<repo>-<task>[-<subtask>]`. Existing skills and memory already use this convention. For a single repo, the default is `~/.agentws/worktrees/<repo>/<task>[-<subtask>]`.
- The tool attaches each worktree that an agent creates to that session. To find them, it uses a hook that runs after `git worktree add`, and it polls `git worktree list`. Subagent worktrees (`.claude/worktrees/agent-*`) attach to the parent session.
- Each repo has a setup recipe, for example: copy an env template and run `bun install`. Worktrees share dependencies through APFS clones or hardlinks. A worktree never gets a fresh install.
- The tool decides cleanup for each worktree, not for each task or session. In a session with 4 PRs, each worktree is removed when its own PR merges.
- When a PR merges, the tool removes its clean worktrees automatically. It does not remove a worktree that is open in a shell, nvim or dev server. The tool saves uncommitted changes as a patch and a tarball of untracked files, then asks you. Detached commits get a backup branch. The tool never deletes local branches.
- A disk view shows the size of each worktree, the total space that cleanup can free, and the free space on the volume.
- At first run, the tool adopts existing worktrees.
- A ports view shows which dev server and port belong to which worktree. One key kills a dev server.

**Review pane**
- A side pane like a PR review: a file tree, a unified or split diff, syntax highlighting, and a marker for the files that you viewed.
- Scope toggle: last agent turn / uncommitted / whole branch vs base.
- A worktree switcher: "all" or a single worktree. In "all", the file tree is grouped by repo, then by subtask and PR.
- Review is local: diffs come from git in each worktree, not from GitHub.
- The tool adds line and range comments to a draft review. The tool sends the draft to the session as one prompt. Each comment has its worktree and file path, so the agent edits the correct checkout.
- Stage or revert each hunk.

**Terminal and nvim**
- One key opens a shell in the selected worktree of the session (or the workspace root), as a split or a popup.
- One key opens the current file and line in nvim. A session can have a long-lived nvim in its own pane. The tool controls it through `nvim --listen`.
- Diffs open in nvim with diffview. The plugin adds comments that you write in nvim to the same draft review.
- `C-h/j/k/l` moves between nvim splits and panes, without the current tmux conflict.

## P1

- A PR board for each session. It shows:
  - checks, with the names of failing jobs linked to their runs
  - review-bot comments since the last push
  - unresolved threads and merge readiness.

  `agentws pr <session> [--json]` shows it. babysit-pr reads it, and does not poll `gh` (see ADR 0024).
- A subagent tree for each session under its sidebar row, from the subagent hooks of the harness. Only Claude has it today. There are no stop and steer controls, because no harness gives them for each subagent (see ADR 0019).
- A Linear launcher: `L` takes one or more issue URLs and starts a worktree and a session for each. A maximum of `[launcher] max_parallel` (default 3) start at the same time. The others wait in a queue and start when sessions finish. See ADR 0031.
- When the Claude quota is low, offer to start queued work in Codex.
- Mouse:
  - Click a session to select it and go to its pane.
  - Click key hints and buttons, select from lists, open review files, put the review cursor or drag a range, and scroll with the wheel.
  - In tmux, a click focuses a pane, a drag on a border resizes, and the wheel scrolls the agent history.
  - Shift-drag (Option in some macOS terminals) keeps the selection of the terminal. `[ui] mouse = false` disables all mouse support (see ADR 0042).
- A first-run walkthrough: select Claude, Codex or both. See the hook state of each, and install it with a backup. Do the trust step of Codex, and get the nvim snippet for the installed plugin. `S` or `agentws setup` opens it again (see ADR 0040).

## P2

- Native macOS client.
- A phone app: a PWA that `agentws serve` serves (see ADR 0046). It lists sessions, chats with them, starts new ones and gets push notifications. It uses the network path that the user selects.
  - While you type in an attached `agentws` tmux client, the tool holds pushes (`[push] away_after`, default 2 minutes, `"0"` disables it).
  - If you leave and a session still needs you, the tool sends the push one time.
  - A device with the app open on screen gets no push.
- Compare the attempts of two agents at the same task, side by side.
- Attach to a remote daemon through SSH.

## Not in v1

- A built-in editor (nvim does this), cloud sandboxes, multiplayer, Docker isolation, Windows.

## Still open

Stack and architecture are decided in [ARCHITECTURE.md](ARCHITECTURE.md).


- Product name (`agentws` is a placeholder).
- Do we port the existing `~/.tmux/agent-hooks` scripts, or do the adapters replace them?
