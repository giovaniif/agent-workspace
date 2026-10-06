# internal/view

The derived view of the daemon's state, so native clients never re-implement `domain` rules. `serve`'s phone stream and the bridge's `view.subscribe` (`agentws rpc`, see [cmd/agentws/](../../cmd/agentws/AGENTS.md)) both use it. See [ADR 0049](../../docs/adr/0049-macos-app.md), "A derived view, shared with serve".

Tests: `go test ./internal/view/ ./internal/serve/ ./cmd/agentws/ -run 'View|Serve|Rpc'`. Goldens are in `testdata/` (`view-subscribe-state.json`, `view-subscribe-diffs.json`); the Mac app's Swift tests decode the same files. Regenerate with `go test ./internal/view -update` and review the diff: a changed golden is a changed API.

## Layers

depguard: imports only `rpc` and `domain` from `internal/`, never `os/exec`. No IO.

## API

- `New(rpc.State) (*View, *State)` and `(*View).Apply(rpc.Diff) []*Diff`: the phone's shapes (`serve` aliases them as `StreamState`, `StreamDiff`, `StreamSession`, `StreamQuota`). Worktrees without `Ports`; events, subagents, drafts and comments dropped. The JSON is pinned by `internal/serve/testdata`.
- `NewNative(rpc.State) (*View, *NativeState)` and `(*View).ApplyNative(rpc.Diff) []*NativeDiff`: the superset `view.subscribe` sends.
  - State: `seq`, `workspaces`, `tasks`, `worktrees` (with `Ports`), `sessions`, `limits`, `queue`, `sends`, `events`, `subagents`, `drafts`.
  - A diff sets `seq` and one or more of the phone diff's fields, plus `event`, `subagent`, `draft` or `comment` as the daemon sent them.
  - A session is the phone's (`name`, `where`, `banner`, `since`) plus `order` and `board`.
  - `order`: its index in `domain.Sidebar`'s rule over one flat list (sessions that need you first, then the rest in the daemon's order); `-1` for an ended session.
  - `board`: the PRs on its worktrees, one per number, in the shape of `agentws pr --json`'s `prs` (ADR 0024); `[]` when there are none.
- A diff that changes another session's derived fields (a PR that renames it, a state change that reorders the list, a removed session) is followed by a `session` diff for it with the same `seq`, in session ID order.
