# Design sources

The v1 TUI mockups: Catppuccin Latte, JetBrains Mono, 1440×900 boards. The PNGs in [`../images`](../images) are exports of these mockups.

- `Main.dc.html`: sessions sidebar, agent pane, shell.
- `Review.dc.html`: review pane with scopes, worktree tabs, diff and draft comments.
- `NewSession.dc.html`: new-session dialog with the low-quota warning.
- `Worktrees.dc.html`: worktrees and disk view.
- `canvas.json`: board layout and titles.

These files are the source files of a Claude Design canvas. The markup of each board is plain HTML with inline styles. Its sample data is in the `renderVals()` script at the bottom. The names in them are placeholders (`ACME-142`, `api`, `web`, `infra`).
