# ADR 0019: Subagent tree

Status: accepted, 2026-09-30.

## Decision

- Claude's `SubagentStart` and `SubagentStop` hooks map to their own harness events, `subagent_start` and `subagent_stop`. `Session.Apply` treats both like `post_tool_use`, so a subagent finishing never ends the parent's turn. `crew setup claude` now installs the `SubagentStart` hook too; existing users re-run it.
- `domain.SubagentFromHook` reads `agent_id`, `agent_type` and, on stop, the first line of `last_assistant_message` (cut to 80 runes). `TrackSubagent` folds a start or stop into the session's list by `(SessionID, ID)`; a stop seen without its start is added as stopped. `EndSubagents` stops a session's running subagents on `session_start` and `session_end`, since nothing it spawned outlives the session. At most 30 per session are kept, dropping the oldest stopped ones first.
- `domain.SubagentTree` orders them parent before children by `ParentID`; an agent with an unknown parent is a root, and a cycle still lists every agent.
- The daemon keeps them in memory and publishes each change as a diff of its own (`Diff.Subagent`), after the session diff of the same hook. `State.Subagents` carries them in the snapshot. They are not stored: a restart drops the running state anyway, because the hooks that would end it are gone.
- The TUI lists a session's subagents under its worktrees (spinner while running, a check once stopped, the summary beside the type), at most 8 rows and then `… N more`. `o` collapses them with the worktrees. Type and summary are agent text and go through `cleanText`.

## Why

- Mapping subagent hooks to `post_tool_use` lost which agent it was; a distinct event keeps the state machine unchanged and gives the tree what it needs.
- Memory only keeps the migration and `Store` port untouched; the tree is a live view, not history.

## Limits

- Claude's hooks name the subagent but not its parent, so every Claude subagent is a root today. `ParentID` is there for a harness that reports it.
- Codex has no subagent hooks (`SubagentStart` and `SubagentStop` stay unmapped), so its sessions show no tree.
- The issue's stop and steer actions are not built. Claude has no per-subagent stop or message entry point outside the model (its keys stop all background agents, or the whole turn), and Codex has none. Sending a stop to the whole pane would hide what it does, so the TUI offers no subagent actions for either harness. That satisfies "unsupported actions are hidden", but leaves the scope item open until a harness exposes one.
- The tree cannot be compared with the harness's own task list by a test; that acceptance check is manual.
