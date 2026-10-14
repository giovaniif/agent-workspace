# ADR 0050: Remove the Linear integration

Status: accepted, 2026-10-13. It supersedes [ADR 0031](0031-linear-launcher.md). It replaces the Linear parts of [ADR 0015](0015-session-lifecycle.md) and [ADR 0026](0026-session-naming.md).

## Context

agentws had three Linear functions: a Linear issue URL as a work item, a title resolver that used the Linear API, and the launcher (`L` in the TUI, a tab in the Mac sheet). The launcher accepted only Linear issue URLs. The owner does not use Linear. The code, the config keys and the tests added cost and gave no value.

## Decision

- agentws has no Linear code. The `linear` adapter, the launcher, its queue (`State.Queue`, `Diff.Queue`) and the methods `launcher.enqueue`, `launcher.drop` and `launcher.retarget` are removed.
- A work item is a GitHub PR URL or text. A different URL is plain text in the preview of the TUI, and `session.new` and `session.resolve` refuse it with `unsupported link: use a GitHub pull request URL`.
- The name of a session comes from the pinned name, then the PR title, then the task text.
- `session.options` does not send `max_parallel`. `config.get` and `config.set` do not know `launcher.max_parallel`.
- An old `config.toml` with a `[launcher]` or `[linear]` table still loads. agentws ignores these tables.
- A task from an old store with the source `linear` keeps its `IssueTitle`, and its name does not change. agentws does not look up its title again.
- The Mac app ignores a saved shortcut for the removed launcher action. The other saved shortcuts stay.

## Consequences

- To start many sessions, start them one at a time with `n` or `agentws new`.
- A request that passes the build handshake and calls a removed launcher method gets `unknown_method`. The build handshake (ADR 0039) stops a request from a different build before the daemon reads its method.
