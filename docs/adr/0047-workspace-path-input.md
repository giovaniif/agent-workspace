# ADR 0047: A typed workspace path in the new-session dialog

Status: accepted, 2026-10-03. Extends [ADR 0037](0037-new-session-popup.md)'s Workspace field; `session.new` is unchanged.

## Decision

- The Workspace field still opens on the registered workspace that holds the launch folder (else the last used one), and `←`/`→` still cycle registered workspaces while nothing is typed.
- Typing turns it into a path. `domain.ParsePathInput` reads `./x`, `../../`, `x`, `~/x` and `/abs` against the folder the popup was opened in (`Options.LaunchDir`), with `Options.Home` for `~`. Emptying it brings the picker back.
- Under the box, a dropdown lists the subfolders of the folder the input points into, filtered by the last segment (`domain.CompleteDirs`: case-insensitive prefix, hidden folders only for a `.` prefix), each marked `repo` or `worktree`. `↑`/`↓` move the highlight, `→` opens it (appends `name/`), `←` goes up a level, `⇥` still moves to the next field.
- The line under the box shows the resolved path and what it is: a registered workspace's kind, `new · added when you create`, or `no such folder`. On create the resolved path is sent as `session.new`'s workspace; the daemon already registers a new folder there.
- The listing comes from a new `workspace.dirs` (`{"path": abs}` → `{"dirs": [Child]}`) call, made once per folder the input points into and answered on the connection's goroutine from `app.WorkspaceFS.Children`. A reply for a folder the input has since left is dropped.

## Why

- Starting in a folder that is not registered needed `crew workspace add` first, or launching crew from inside it. The dialog is where the choice is made, so it should take any folder.
- The TUI does no IO of its own (ADR 0037), so the daemon lists folders, as it already does for discovery.
