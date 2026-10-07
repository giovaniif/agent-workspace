# internal/tui

Bubble Tea v2, Lip Gloss and Bubbles. The TUI talks only to `rpc.Client` (it may import `domain` types) and never runs git, gh or tmux on the render path. See [ADR 0008](../../docs/adr/0008-tui-shell.md).

## Tests

- Goldens live in `testdata/*.golden`; regenerate with `go test ./internal/tui/ -run Golden -update` and review the diff. The setup walkthrough's are `testdata/TestGoldenSetup/*.golden`.
- `BenchmarkReviewScroll` fails if a review frame takes over 16 ms p95 (about 2 ms).
- Worktrees and disk view: `go test ./internal/tui/... -run Worktrees` (the daemon side is in [internal/daemon/](../daemon/AGENTS.md)).
- By hand, run against a temp `AGENTWS_HOME` and `AGENTWS_TMUX_SOCKET`, seeded with `agentws debug seed N`.

## Sidebar

The client layout's left pane runs `agentws tui`, 48 columns wide; a `window-resized` hook on the window puts it back to 48 when the terminal resizes.

`agentws tui` opens two connections: one subscribes and feeds diffs to the Bubble Tea program, the other makes calls such as `client.focus_main`, so a burst of diffs never delays a keypress. The model keeps the snapshot in maps and rebuilds the sidebar rows only when a diff arrives; grouping and order come from `domain.Sidebar` (sessions that need you first in each task group). Keys only move the selection, and `View` renders from memory. One 200 ms ticker drives every running spinner and the clock. The renderer runs at 120 fps: at the default 60 a key can wait a whole 16 ms frame before it is drawn.

- `M` and `E` open the model and effort pickers; the choice is sent as `session.switch`, and the sidebar shows unconfirmed switches as `→ value` and a `!` when a harness did not confirm one. The new-session model control is the same for every harness: ←/→ cycle `‹ model ›`. Only omp also takes typed text, and that opens a menu on the finished screen, as wide as the longest model, above the field when the rows below would be cut. ctrl-n and ctrl-p move, ctrl-y or enter accepts into the `‹ ›` control, and ctrl-e drops what was typed and restores that control (the nvim insert-completion keys). `M` filters omp's catalog as you type, and enter on no match applies the typed id. The catalog arrives after the first frame. A long id is cut at its column so it does not run into the next one.
- The sidebar has no card panel: under the list there is only the footer (key hints, then counts and `next waiting`). `domain.BuildSessionCard` still feeds the review's agent column; see [ADR 0014](../../docs/adr/0014-session-card.md). The sidebar cuts long names with an ellipsis; the review's agent column shows the full name.
- Under the top bar, one row per harness shows its quota windows (percent used, reset clock time; see ADR 0036), derived from the sessions' `Limits` by `domain.Quotas`; red below 20% left, dimmed with an age when older than 15 minutes, absent without data. See [ADR 0017](../../docs/adr/0017-usage-and-limits-bar.md).
- A session is one row; only the selected one adds a line with model, effort and worktree count. Worktrees have no rows of their own (they are in the review and `w`).
- Running subagents show under their session as a tree (`domain.SubagentTree`, at most 8 rows); finished ones collapse into one `✓ N subagents done` line on the selected session. `o` hides them.
- The mouse wheel scrolls the list without moving the selection; the next key that moves the selection brings it back into view, and a click selects a row without moving it. A click on a resume picker row (`u`) resumes and focuses that session, like `enter` on it.
- **Projects.** With registered projects, a `PROJECTS` section sits above `SESSIONS`: one row per project (`domain.ProjectRows`, by name) with the count of worktrees it holds, so worktrees left by ended sessions stay visible. A click on a row opens the project. `P` opens the projects panel: `j`/`k` move, `enter` opens, `a` adds (path, then `tab` to the optional setup script; `project.add`'s refusal shows under the form), `d` then `y` removes (`project.remove`). Opening a project opens the new-session dialog in its root: the popup gets `AGENTWS_PROJECT=<root>` in its env and `agentws tui --new-session` passes it as `Options.Project`, which wins over the last-created and launch-dir defaults. Tests: `go test ./internal/tui/ -run Project`.
- Ports show on the session row and the status line. `K` asks (`y`) before killing the selected session's dev servers.
- The new-session dialog's Workspace field takes a typed path (`./`, `../`, `~/`, `/`) read from the launch folder (`Options.LaunchDir`, with `Options.Home` for `~`), with a dropdown of subfolders from `workspace.dirs`: `↑`/`↓` pick, `→` open, `←` up. A failed listing is retried on the next edit. See [ADR 0047](../../docs/adr/0047-workspace-path-input.md).
- `q` and ctrl+c detach the tmux client through `client.detach` and leave the sidebar running. Once the subscription has ended (`DisconnectedMsg`) they quit at once without calling the daemon (`q` only outside a text field, ctrl+c anywhere), and a detach that fails on a closed connection or `version_mismatch` quits too, so the sidebar can always be closed. Tests: `go test ./internal/tui/ -run Quit`.
- **Reconnecting.** When the subscription ends, the sidebar shows `● daemon disconnected` under the limits and redials in a command (`Options.Redial`, `tui.Redialer`: two `rpc.Dial`s and a `subscribe`, never a daemon start; the `tui` package cannot exec) after `domain.ReconnectDelay` (500 ms doubling to 5 s), with the try number and the seconds to it on the second line. A daemon of the same build replaces the state, its diffs are followed (`listen`, one command per diff) and every port in `Options` moves to the new connection. A `version_mismatch` stops the retries and shows `agentws was upgraded`; with `Options.CanRestart` (only the main sidebar), `r` quits with `tui.ErrRestart` and `agentws tui` re-execs itself, picking up the installed binary. Popups (`--new-session`, `--setup`) do not redial. Tests: `go test ./internal/tui/ -run 'Reconnect|Disconnected|AnotherBuild'` and `test/e2e/testdata/script/tui_reconnect.txtar`.
- Other keys: `n` new session (popup via `client.popup`, ADR 0037), `enter` focus, `x` then `y` end, `u` resume an ended session (a picker, most recently active first), `m` mute, `R` rename and pin, `A` unpin, `L` launcher, `P` projects, `r` review, `w` worktrees and disk, `t` shell, `T` shell popup, `s` focus the shell, `e` nvim, `S` setup walkthrough. `ctrl+\` (`tmux.FocusSidebarKey`) returns to the sidebar from an agent pane ([ADR 0025](../../docs/adr/0025-focus-return-key.md)).

## Config

Colors are Catppuccin Latte, overridden per key in the `[theme]` table of `$AGENTWS_HOME/config.toml` (`text`, `subtext`, `overlay`, `surface`, `mantle`, `base`, `blue`, `peach`, `green`, `red`, `teal`, `mauve`, `selected`, `added_bg`, `deleted_bg`), read once at startup. Review syntax colors come from the same keys. The same file holds `[defaults.claude]`, `[defaults.codex]` and `[defaults.omp]` tables with `model` and `effort`, the starting values for the new-session dialog (`tui.LoadDefaults`).

## Mouse

- On unless `[ui] mouse = false` (`tui.LoadMouse` sets `Options.NoMouse`). Each screen's layout function returns an owner per row beside its lines (`mainScreen`, `dialogScreenLines`, `diskScreenLines`, `treeLines`); `View` ignores them and a click re-runs the layout and looks the row up, so rendering pays nothing. The review is hit-tested by its fixed bands. A click on a key hint fires that key through the normal key path; only footers, help rows and button rows (they name `esc`) are hints, and while a text field is focused only non-printing keys fire. See [ADR 0042](../../docs/adr/0042-mouse.md).
- Tests: `go test ./internal/tui/ -run Mouse -bench Mouse`. By hand, run `agentws tui` in a pane of a throwaway `tmux -L x` server with a temp `AGENTWS_HOME` and `AGENTWS_TMUX_SOCKET`, and send SGR mouse sequences with `send-keys -l` (`\e[<0;COL;ROWM`, then the same ending in `m`).

## Review viewer

See [ADR 0023](../../docs/adr/0023-review-pane.md). `r` opens the review of the selected session; the sidebar pane widens to 75% of the window (`client.review`) and collapses to a rail. The TUI highlights and lays out the `review.open` answer in the command that fetched it.

- File tree grouped by worktree with its PR, unified or split diff, hunk headers, a `✓` per viewed file, a line cursor.
- Keys: `[`/`]` scope, `w` worktree (all, then each), `n`/`p` file, `j`/`k` line, `u` split, `v` viewed, `c` comment, `V` range, `S` send, `s` stage hunk, `x` revert hunk (after `y`), `o` open the diff's top visible line in nvim and close the review, `r` or `esc` close.
- **Syntax.** chroma's lexer engine with a curated set of its lexers: new lexers go in [internal/syntax/lexers/](../syntax/lexers) as chroma XML files. depguard bans chroma's `lexers` and `styles` packages (their init costs every hook).

## Disk view

`w` opens it. See [ADR 0030](../../docs/adr/0030-worktrees-disk-view.md).

- **Header.** Volume free and total, worktree total, reclaimable total (`domain.Reclaimable`: rows the engine would remove or back up and remove), the auto-cleanup interval and the shared deps store size. A total that misses sizes still being measured ends in `+`.
- **Rows.** One per worktree from `disk.view`; a size still being measured shows `…`. Nothing on the render path waits for `du`.
- **Actions.** `d` and `b` (after `y`) call `cleanup.worktree`: `d` removes only merged clean worktrees, `b` backs up a dirty or detached one first, and both end with the engine's last process and repo-state checks. They really remove worktrees, so try them only against temp repos. `k` kills the row's dev servers, `g` goes to its session, `o` opens its shell through `shell.toggle` with just the worktree.
- **Recently cleaned.** The last audit log lines (`fs.AuditLog.Recent`).
- **Refresh.** Every 2 s while sizes are pending and every 10 s after.
