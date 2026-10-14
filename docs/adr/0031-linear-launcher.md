# ADR 0031: Linear launcher

Status: superseded by [ADR 0050](0050-remove-linear.md) on 2026-10-13. Was accepted 2026-09-30.

## Decision

- `L` in the TUI opens a multi-line input. Paste issue URLs, or type them with `ctrl+j` between lines; whitespace separates them. `domain.ParseIssueURLs` keeps the Linear issue URLs once each, and the input shows how many it found and how many words were not Linear URLs. `enter` calls `launcher.enqueue` (`{"workspace","input","harness","model","effort"}` → `{"queued":[refs],"rejected":[words]}`); the TUI sends Claude with the `[defaults.claude]` model and effort and leaves the workspace empty, so the last used one is taken when the session starts.
- The queue lives in the daemon, so closing the TUI does not lose it. `State.Queue` and `Diff.Queue` (a whole-queue replace) carry it. Each `domain.LaunchItem` is waiting, `Starting`, or failed (`Err`).
- `domain.DrainLauncher(queue, active, limit)` is the rule: it marks as starting as many waiting items as fit in `limit` minus the active and starting ones. `domain.ActiveLaunched` counts the launcher's own sessions that are on a pane and not `done`. A session that stops frees its slot even if it is prompted again later; a session ended with `x` frees it too. Sessions started with `n` do not count.
- A worker goroutine drains. It is woken (non-blocking) when something is enqueued and on every session change while the queue is not empty. Starting an item runs `session.new`'s code (`Daemon.startSession`) off the loop, so each session gets its worktree, setup recipe and task exactly as `n` would, and is named after its issue by the existing title resolver (ADR 0026).
- The limit is `max_parallel` in the `[launcher]` table of `$AGENTWS_HOME/config.toml`; default 3 (`domain.DefaultMaxParallel`).
- A failed start stays in the queue with its error and takes no slot. Enqueuing the same issue again retries it. An issue already waiting, or with a launched session still working, is not queued twice.
- `launcher.drop` (`{"id"}`) removes a waiting or failed item and `launcher.retarget` (`{"id","harness","model","effort"}`) changes a waiting one; neither touches an item that is starting. In the TUI `X` drops every item that is not starting and `c` retargets every waiting item that has a Codex offer.
- Low-quota fallback: the queue section of the sidebar calls `domain.OfferFallbacks` with the waiting items' requests and renders the offer under each item (`↳ c → codex 64% gpt-5`), using the same `[fallback]` config as the dialog (ADR 0027).
- `git worktree add` runs one at a time in the daemon (`serialAdder`). Two runs on one repo race on its config file and one fails with "unable to write upstream branch configuration"; the launcher hit this with three at once. Setup recipes and pane creation still run in parallel.

## Why

- The slot rule is pure and table-tested; the daemon only applies it.
- A daemon-side queue drains with no TUI attached, which is the point of queueing work.

## Limits

- The queue and the launcher's session set are in memory. After a daemon restart the queue is gone and running sessions stop counting against the limit.
- Finished means the agent stopped (`done`). A session waiting on you still holds its slot.
- Each session names itself from the URL slug until the Linear lookup lands; without a `[linear] token` it keeps the slug title.
- Workspace choice is the last used one. There is no per-issue workspace yet.
- The offer is taken as a whole: `c` moves every offered item, not a chosen one.
