# internal/tui

Bubble Tea v2, Lip Gloss and Bubbles. The TUI talks only to `rpc.Client`, and it can import `domain` types. It never runs git, gh or tmux on the render path. See [ADR 0008](../../docs/adr/0008-tui-shell.md).

## Tests

- Goldens are in `testdata/*.golden`. To make them again, run `go test ./internal/tui/ -run Golden -update` and review the diff. The goldens of the setup walkthrough are `testdata/TestGoldenSetup/*.golden`.
- `BenchmarkReviewScroll` fails if a review frame takes more than 16 ms p95 (about 2 ms).
- Worktrees and disk view: `go test ./internal/tui/... -run Worktrees` (the daemon side is in [internal/daemon/](../daemon/AGENTS.md)).
- For runs by hand, use a temp `AGENTWS_HOME` and `AGENTWS_TMUX_SOCKET`, seeded with `agentws debug seed N`.

## Sidebar

The left pane of the client layout runs `agentws tui`, 48 columns wide. When the terminal resizes, a `window-resized` hook on the window sets it back to 48.

`agentws tui` opens two connections. One subscribes and sends diffs to the Bubble Tea program. The other makes calls, for example `client.focus_main`. Thus a burst of diffs never delays a keypress.

- The model keeps the snapshot in maps. It builds the sidebar rows again only when a diff arrives.
- Grouping and order come from `domain.Sidebar`: in each task group, sessions that need you come first.
- Keys only move the selection, and `View` renders from memory.
- One 200 ms ticker controls all running spinners and the clock.
- The renderer runs at 120 fps. At the default 60 fps, a key can wait a full 16 ms frame before it is drawn.

- `M` and `E` open the model and effort pickers. The TUI sends the choice as `session.switch`. The sidebar shows unconfirmed switches as `→ value`, and a `!` when a harness did not confirm one.
- The new-session model control is the same for all harnesses: ←/→ cycle `‹ model ›`.
  - Only omp also takes typed text. Typed text opens a menu on the finished screen, as wide as the longest model. If the rows below would be cut, the menu shows above the field.
  - ctrl-n and ctrl-p move. ctrl-y or enter accepts into the `‹ ›` control. ctrl-e removes the typed text and restores that control. These are the nvim insert-completion keys.
  - `M` filters the catalog of omp as you type. If there is no match, enter applies the typed id.
  - The catalog arrives after the first frame. A long id is cut at its column, so it does not go into the next column.
- The sidebar has no card panel. Under the list there is only the footer: key hints, then counts and `next waiting`. `domain.BuildSessionCard` still supplies the agent column of the review (see [ADR 0014](../../docs/adr/0014-session-card.md)). The sidebar cuts long names with an ellipsis. The agent column of the review shows the full name.
- Under the top bar, one row for each harness shows its quota windows: percent used and reset clock time (see ADR 0036). `domain.Quotas` derives them from the `Limits` of the sessions. A window is red below 20% left. It is dimmed with an age when it is older than 15 minutes. It is absent when there is no data. See [ADR 0017](../../docs/adr/0017-usage-and-limits-bar.md).
- A session is one row. Only the selected session adds a line with model, effort and worktree count. Worktrees have no rows of their own: they are in the review and in `w`.
- Running subagents show under their session as a tree (`domain.SubagentTree`, a maximum of 8 rows). On the selected session, finished subagents collapse into one `✓ N subagents done` line. `o` hides them.
- The mouse wheel scrolls the list and does not move the selection. The next key that moves the selection shows it again. A click selects a row and does not scroll. A click on a resume picker row (`u`) resumes and focuses that session, as `enter` on it does.
- **Projects.** With registered projects, a `PROJECTS` section is above `SESSIONS`. It has one row for each project (`domain.ProjectRows`, by name) with the count of its worktrees. Thus worktrees from ended sessions stay visible. A click on a row opens the project.
  - `P` opens the projects panel: `j`/`k` move, `enter` opens, `d` then `y` removes (`project.remove`).
  - `a` adds a project: the path, then `tab` to the optional setup script. If `project.add` refuses it, the refusal shows under the form.
  - To open a project opens the new-session dialog in its root. The popup gets `AGENTWS_PROJECT=<root>` in its env. `agentws tui --new-session` passes it as `Options.Project`, which has priority over the last-created and launch-dir defaults.
  - Tests: `go test ./internal/tui/ -run Project`.
- **Tabs.** A selected session can have its strip in a project worktree (`domain.TabHome`: a worktree that it holds, or the worktree that it is a tab of). Such a session adds a tab line under its facts line: `[1 ○ claude]  2 shell  3 ✳ codex`. The line comes from `domain.WorktreeTabs`, and the bracketed tab comes from `State.active_tabs`.
  - A click on a tab calls `tab.show`. `[`/`]` call `tab.step`.
  - `-` asks (`y`) and calls `tab.close` on the bracketed tab.
  - `+` opens the NEW TAB chooser: `shell`, then one row for each harness with `‹ model ›` and `‹ effort ›` from `[defaults.<harness>]`. In it, `j`/`k` select the row, `←`/`→` change, `tab` selects model or effort, `enter` calls `tab.new` and `esc` goes back.
  - Agent tabs are sessions, so each one also has its own row in its task group.
  - Sessions outside projects show no tab line, and these keys do nothing.
  - The context menu adds "New tab" and "Close tab" only for sessions with tabs (`menuActions`). Tests: `go test ./internal/tui/ -run Tab`; the daemon side is in [internal/daemon/](../daemon/AGENTS.md#tabs).
- Ports show on the session row and the status line. `K` asks (`y`) before it kills the dev servers of the selected session.
- The Workspace field of the new-session dialog takes a typed path (`./`, `../`, `~/`, `/`). It reads the path from the launch folder (`Options.LaunchDir`, with `Options.Home` for `~`). A dropdown shows subfolders from `workspace.dirs`: `↑`/`↓` select, `→` opens, `←` goes up. If a listing fails, the TUI tries it again on the next edit. See [ADR 0047](../../docs/adr/0047-workspace-path-input.md).
- `q` and ctrl+c detach the tmux client through `client.detach`, and the sidebar continues to run. The sidebar can always close:
  - After the subscription ended (`DisconnectedMsg`), they quit immediately and do not call the daemon. `q` works only outside a text field, ctrl+c works everywhere.
  - If a detach fails on a closed connection or on `version_mismatch`, they also quit. Tests: `go test ./internal/tui/ -run Quit`.
- **Reconnecting.** When the subscription ends, the sidebar shows `● daemon disconnected` under the limits.
  - After `domain.ReconnectDelay` (500 ms, doubled each time to 5 s), it dials again in a command (`Options.Redial`, `tui.Redialer`). This is two `rpc.Dial`s and a `subscribe`, never a daemon start. The `tui` package cannot exec.
  - The second line shows the try number and the seconds until the next try.
  - A daemon of the same build replaces the state. The TUI follows its diffs (`listen`, one command for each diff), and all ports in `Options` move to the new connection.
  - A `version_mismatch` stops the retries and shows `agentws was upgraded`. With `Options.CanRestart` (only the main sidebar), `r` quits with `tui.ErrRestart`. `agentws tui` then re-execs itself and gets the installed binary.
  - Popups (`--new-session`, `--setup`) do not dial again. Tests: `go test ./internal/tui/ -run 'Reconnect|Disconnected|AnotherBuild'` and `test/e2e/testdata/script/tui_reconnect.txtar`.
- Other keys: `n` new session (popup through `client.popup`, ADR 0037), `enter` focus, `x` then `y` end, `u` resume an ended session (a picker, most recently active first), `m` mute, `R` rename and pin, `A` unpin, `L` launcher, `P` projects, `r` review, `w` worktrees and disk, `t` shell, `T` shell popup, `s` focus the shell, `e` nvim, `S` setup walkthrough. `ctrl+\` (`tmux.FocusSidebarKey`) returns to the sidebar from an agent pane ([ADR 0025](../../docs/adr/0025-focus-return-key.md)).

## Config

Colors are Catppuccin Latte. You can override each key in the `[theme]` table of `$AGENTWS_HOME/config.toml` (`text`, `subtext`, `overlay`, `surface`, `mantle`, `base`, `blue`, `peach`, `green`, `red`, `teal`, `mauve`, `selected`, `added_bg`, `deleted_bg`). The TUI reads it one time at startup. Review syntax colors come from the same keys. The same file holds `[defaults.claude]`, `[defaults.codex]` and `[defaults.omp]` tables with `model` and `effort`. These are the start values for the new-session dialog (`tui.LoadDefaults`).

## Mouse

- The mouse is on, unless `[ui] mouse = false` (`tui.LoadMouse` sets `Options.NoMouse`).
  - The layout function of each screen returns an owner for each row beside its lines (`mainScreen`, `dialogScreenLines`, `diskScreenLines`, `treeLines`). `View` ignores them. A click runs the layout again and finds the row. Thus rendering has no extra cost.
  - The review finds the click target by its fixed bands.
  - A click on a key hint fires that key through the normal key path. Only footers, help rows and button rows (they name `esc`) are hints. While a text field has focus, only non-printing keys fire. See [ADR 0042](../../docs/adr/0042-mouse.md).
- A right-click on a session row selects it and opens a small context menu under it (`contextmenu.go`).
  - Its entries are the fixed list `sessionMenuActions`. Each entry is a label and the key that it fires through the normal key path. For example, "End session" fires `x`, so the same y/n confirm follows. To add an entry, add it there.
  - Entries marked `tabs` show only for a session with tabs.
  - ↑/↓ and enter select. Any other key, a click outside the menu or a selection change closes it.
  - Task headers own `h:` rows, so a right-click on them does nothing.
  - Tests: `go test ./internal/tui/ -run ContextMenu`.
- Tests: `go test ./internal/tui/ -run Mouse -bench Mouse`. For a test by hand:
  1. Run `agentws tui` in a pane of a throwaway `tmux -L x` server, with a temp `AGENTWS_HOME` and `AGENTWS_TMUX_SOCKET`.
  2. Send SGR mouse sequences with `send-keys -l` (`\e[<0;COL;ROWM`, then the same sequence that ends in `m`).

## Review viewer

See [ADR 0023](../../docs/adr/0023-review-pane.md). `r` opens the review of the selected session. The sidebar pane widens to 75% of the window (`client.review`), and the list collapses to a rail. The TUI highlights and lays out the `review.open` answer in the command that got it.

- A file tree grouped by worktree with its PR, a unified or split diff, hunk headers, a `✓` for each viewed file and a line cursor.
- Keys: `[`/`]` scope, `w` worktree (all, then each), `n`/`p` file, `j`/`k` line, `u` split, `v` viewed, `c` comment, `V` range, `S` send, `s` stage hunk, `x` revert hunk (after `y`), `o` open the diff's top visible line in nvim and close the review, `r` or `esc` close.
- **Syntax.** The lexer engine of chroma, with a curated set of its lexers. Put new lexers in [internal/syntax/lexers/](../syntax/lexers) as chroma XML files. depguard bans the `lexers` and `styles` packages of chroma, because their init adds time to each hook.

## Disk view

`w` opens it. See [ADR 0030](../../docs/adr/0030-worktrees-disk-view.md).

- **Header.** Volume free and total, the worktree total, the reclaimable total, the auto-cleanup interval and the size of the shared deps store. The reclaimable total (`domain.Reclaimable`) counts the rows that the engine would remove, or back up and remove. If a total does not include sizes that are still in measurement, it ends in `+`.
- **Rows.** One for each worktree from `disk.view`. A size that is still in measurement shows `…`. Nothing on the render path waits for `du`.
- **Actions.**
  - `d` and `b` (after `y`) call `cleanup.worktree`. `d` removes only merged clean worktrees. `b` first backs up a dirty or detached worktree. Both end with the last process and repo-state checks of the engine.
  - **CAUTION:** `d` and `b` really remove worktrees. Try them only on temp repos.
  - `k` kills the dev servers of the row. `g` goes to its session. `o` opens its shell through `shell.toggle`, with only the worktree.
- **Recently cleaned.** The last audit log lines (`fs.AuditLog.Recent`).
- **Refresh.** Every 2 s while sizes are pending and every 10 s after.
