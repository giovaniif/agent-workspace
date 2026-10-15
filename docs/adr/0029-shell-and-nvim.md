# ADR 0029: Shell and nvim panes, the nvim plugin and draft comments

Status: accepted, 2026-09-30.

## Decision

- **Shell.** `shell.toggle` `{session, worktree, popup}` finds or creates the shell for `(session, worktree)` and shows or hides it. `session` may be empty when `worktree` names one: the worktree's owner is then the session, and an unowned worktree gets a shell keyed by the worktree alone. The disk view's shell action uses this too. `domain.ChooseShell` picks the directory: the wanted worktree, else the session's first by ID, else the cwd its hooks last reported. The daemon keeps `key → pane` in memory and checks `Alive` before reuse, so a shell that exited is replaced.
- **Split.** The shell is joined below the agent pane (`join-pane -d -v -l 35%`) and hidden with `break-pane -d`, so it keeps running in a window of its own. Showing it leaves keyboard focus in the sidebar (#90), so `t` again hides it; `s` (`shell.focus`) shows it if needed and moves focus into it, and `ctrl+\` comes back. Toggling shows the session's agent pane first, without moving focus, unless the session's nvim is in the slot.
- **Popup.** `T` runs `display-popup -E` on the attached client. The popup runs a second client on a grouped session (`crew-popup-*`, `destroy-unattached on`) with the shell's window selected, so the shell is the same one the split shows and survives the popup. `M-t` detaches the popup client; a shell that exits closes it too.
- **nvim.** One per session, started `nvim --listen $CREW_HOME/nvim/<session>.sock` in a pane with `CREW_SESSION` and `CREW_HOME` set. `nvim.toggle` (`e`) swaps it into the main slot and back. `nvim.open` (`o` in the review) starts it on `+<line> <file>`, or, when it is running, evaluates `execute('edit +N ' . fnameescape('<file>'))` through `nvim --server <sock> --remote-expr`, then shows and focuses it. The review closes first so nvim gets the room. A path must be inside the worktree.
- **Keys.** `t` shell, `T` popup, `s` focus the shell, `e` nvim, `o` in the review. The worktree is the review's last `w` choice for the selected session, else the session's first.
- **Navigation.** The tmux config binds no `C-h/j/k/l`, so every pane, agents included, gets those keys. Only the plugin acts on them, inside nvim: `wincmd`, and at nvim's edge `tmux if -F '#{pane_at_<edge>}' '' 'select-pane -<dir>'`, so focus never wraps. This is the vim-tmux-navigator pattern minus the tmux-side binding: keys reach a non-nvim pane untouched and only nvim navigates.
- **Plugin** (`nvim/`). It calls the CLI, never the socket. `:CrewComment` (range, optional text, else `vim.ui.input`) runs `crew review comment --file <realpath> --start --end --code --body`. `:CrewDiff [scope]` runs `crew review scope`, picks the worktree holding the buffer (else the first with a base) and runs `:DiffviewOpen <from> --untracked-files=true -C<path>`. The session comes from `$CREW_SESSION`. `setup{bin, tmux, navigate}` maps `C-h/j/k/l`.
- **Scope.** `WorktreeReview.From` is the commit the diff starts from. `review.open` with no scope uses the session's last opened one (`uncommitted` at first), so `:CrewDiff` shows what the TUI shows.
- **Draft comments.** `review.comment` `{session, file | worktree+path, start_line, end_line, code, body}` places the file in one of the session's worktrees (`domain.ResolveCommentFile`, innermost wins), adds it to the session's review draft and emits it as `Diff.Comment` with `Diff.Draft`. `State.Drafts` carries drafts for late subscribers. (ADR 0028 replaced the in-memory `DraftComment` with the persisted draft.) The review shows `N draft comments`.
- **Adapter.** `adapters/nvim` runs `nvim --server`; `app.Editor` is its port. `adapters/tmux` gains `ShowBelow`, `HideBelow`, `BelowPane`, `Popup`.
- **Tests.** Plugin specs (`nvim/test/spec.lua`) run headless with `--clean` and throwaway XDG dirs, against a fake `crew`, `tmux` and `DiffviewOpen`. `TestNvimPluginSpecs` runs them in `test/integration`.

## Why

- `nvim --remote +N file` treats `+N` as a file. An expression with `fnameescape` opens any path at a line without a shell or `:` command in between.
- Calling the CLI keeps the plugin free of a socket client and follows the architecture note.
- A grouped session gives the popup its own current window, so showing the shell there does not change what the main client shows. A plain `attach -t <window>` would.
- `display-message -t <window>.2` falls back to another pane when the index is missing, so `BelowPane` lists panes.
- The no-wrap guard matters: tmux's `select-pane -D` at the bottom wraps to the top.

## Rejected

- **Talking to nvim over msgpack-rpc from the daemon:** more code than one `--remote-expr`.
- **`display-popup` running the shell directly:** the shell would die with the popup and could not be the one the split shows.
- **Persisting shell panes across daemon restarts:** needs a pane option to recover the key; not worth it yet. A shell left by a dead daemon stays in its parked window until the tmux server ends.
- **A worktree cursor in the sidebar:** the review's `w` filter already names a worktree.

## Consequences

- From a pane that is not nvim there is no `C-h/j/k/l` navigation, because those keys belong to the app (`C-k`/`C-l` are readline keys in Claude Code's prompt). `enter` in the sidebar focuses the agent pane, and the return-to-sidebar key from the focus issue leaves it.
- A shell kept alive in a worktree is seen by cleanup's lsof holder check (`procs.Table.Holders` lists every process whose cwd is inside the path, shells in tmux panes included), so cleanup will not remove that worktree. No new code was needed.
- `M-t` needs Option-as-Meta in macOS Terminal and iTerm.
- Superseded by ADR 0028: drafts are persisted, sent and archived there.
- `o` opens the top visible row of the diff, not a cursor line: the review has no cursor yet.
