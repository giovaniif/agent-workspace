# internal/view

The derived view of the daemon's state. With it, native clients never implement `domain` rules again. The phone stream of `serve` and the `view.subscribe` of the bridge (`crew rpc`, see [cmd/crew/](../../cmd/crew/AGENTS.md)) both use it. See [ADR 0049](../../docs/adr/0049-macos-app.md), "A derived view, shared with serve".

Tests: `go test ./internal/view/ ./internal/serve/ ./cmd/crew/ -run 'View|Serve|Rpc'`. Goldens are in `testdata/` (`view-subscribe-state.json`, `view-subscribe-diffs.json`). The Swift tests of the Mac app decode the same files. To make them again, run `go test ./internal/view -update` and review the diff. A changed golden is a changed API.

## Layers

depguard: from `internal/`, the package imports only `rpc` and `domain`. It never imports `os/exec`. It does no IO.

## API

- `New(rpc.State) (*View, *State)` and `(*View).Apply(rpc.Diff) []*Diff`: the shapes of the phone (`serve` aliases them as `StreamState`, `StreamDiff`, `StreamSession`, `StreamQuota`). Worktrees have no `Ports`. Events, subagents, drafts and comments are not included. `internal/serve/testdata` pins the JSON.
- `NewNative(rpc.State) (*View, *NativeState)` and `(*View).ApplyNative(rpc.Diff) []*NativeDiff`: the superset `view.subscribe` sends.
  - State: `seq`, `workspaces`, `projects`, `tasks`, `worktrees` (with `Ports`), `sessions`, `limits`, `sends`, `events`, `subagents`, `drafts`, `reclaimable` (`{"size","pending"}`). `reclaimable` is the disk space that the next cleanups free, from the last `disk.view`. It is zero until one `disk.view` ran. A diff sets it when it changes.
  - `mates` (also in the phone state): `{"stopped","mates":[...]}`. `stopped` is the kill switch. Each mate is the `domain.MateStatus` of the daemon (`name`, `state`, `error`, `guard`) and these fields:
    - `threads`: its Slack threads, newest first. Each is the `domain.MateLink` (with the Slack `url`) and `session` (the derived name), `state` and `ended` of the session.
    - `work`: the IDs of its thread sessions that are not ended and that run, wait or ask for permission (`domain.MateWorking`). This is the current work of the mate.
    - A diff sets the whole `mates` when the board, a thread link or a thread session changes it. It comes after the other session diffs of the same `seq`, so the names are new.
  - A diff sets `seq` and one or more fields of the phone diff. It also sets `project`, `removed_project`, `event`, `subagent`, `draft` or `comment` as the daemon sent them. The phone stream does not include projects.
  - A session is the phone's (`name`, `where`, `banner`, `since`) plus `order` and `board`.
  - `order`: its index from the rule of `domain.Sidebar` over one flat list. Sessions that need you come first, then the others in the daemon's order. An ended session has `-1`.
  - `board`: the PRs on its worktrees, one for each number, in the shape of the `prs` of `crew pr --json` (ADR 0024). It is `[]` when there are none.
- Some diffs change the derived fields of a different session: a PR that renames it, a state change that changes the list order, or a removed session. After such a diff, a `session` diff for that session follows with the same `seq`, in session ID order.
