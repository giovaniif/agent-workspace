# ADR 0038: Title strips on tmux pane borders

Status: accepted, 2026-09-30.

## Decision

- The crew tmux config sets `pane-border-status top` and `pane-border-format "#{?@crew_title, #{@crew_title} ,}"`, so every pane has a top border, and one with the `@crew_title` pane option set shows that text in it.
- `app.TerminalHost.SetTitle(pane, title)` sets the option (`set-option -p`).
- Each agent pane's title is `domain.AgentTitle(session, worktrees, cwd)`, as in `docs/images/sessions.png`: `◐ claude · opus-5.5 · high · cwd platform │ api:org-scope #3611 ◐ │ web:share-token #5731 ✓ │ api:legacy-removal no PR`. The state mark and PR marks are the sidebar's; `cwd` is the session's last hook cwd.
- A daemon worker (`paneTitles`) looks every 250 ms. Only when the state changed does it copy the sessions on a pane and the worktrees on the loop; it formats the titles and runs `SetTitle` on its own goroutine, only for those that changed.
- The shell split's title is `domain.ShellTitle`: `shell · api:org-scope · /path · t hide · s type · T popup`, set when the daemon shows the shell (#92).

## Why

- The agent pane is the harness's own process, so crew cannot draw a header inside it; a tmux border title is drawn by tmux and costs no extra pane or process.
- Titles are pure domain output, so they are table-tested and the worker only diffs strings.

## Limits

- `pane-border-status` is a window option, so the sidebar also gets a top border row, blank.
- The border is plain text in one style: the mockup's highlighted tabs and check counts (`12/14`) are not shown; a PR shows `✓`, `✗`, `◐` or `merged` from its check state.
- A title can lag a change by up to 250 ms.
