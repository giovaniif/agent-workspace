# ADR 0026: Session naming

Status: accepted, 2026-09-30. The Linear parts are superseded by [ADR 0050](0050-remove-linear.md) on 2026-10-13.

## Decision

- A session's name comes from its work item, never from what the agent is doing. `domain.NameFor(task, prs)` picks the first non-blank of: the pinned name, the titles of the PRs on the session's worktrees, `Task.PRTitle` (the PR the task was started from), `Task.IssueTitle` (Linear), then `domain.SummarizeText(task.Text)`.
- `SummarizeText` is the local heuristic for text tasks, with no LLM call: the first line, leading filler dropped (`please`, `can you`, `let's`, `help me`, ...), at most 4 words, cut at the first sentence end, and a trailing connective (`the`, `to`, `in`, ...) dropped. A prompt that is all filler keeps its own words.
- Titles are resolved by `app.TitleResolver` (`Title(ctx, task) (string, error)`), chained by `app.TitleResolvers` (first non-empty answer wins, errors only surface when nobody answered). `adapters/linear` asks the Linear GraphQL API with a personal API key; `adapters/github` runs `gh pr view <url> --json title`. Both answer nothing for tasks they do not own, and `linear` answers nothing without a token.
- The token is `token` in the `[linear]` table of `$AGENTWS_HOME/config.toml`, the file that already holds `[theme]`. It is optional; without it a Linear task keeps the title guessed from the URL slug.
- After `session.new` creates a task for a Linear or PR link, a worker resolves the title (10 s timeout) and the loop merges it into the task as it is at that moment (`domain.WithTitle`), so a pin made while the lookup ran is kept. A failed lookup is logged and changes nothing. Existing tasks are not looked up again.
- A PR that appears later needs no lookup: worktree detection already fills `Worktree.PR`, and `NameFor` puts it before the issue title on the next render.
- `session.rename` (`{"id","name"}`) pins a name on the session's task and `session.unpin` (`{"id"}`) clears it, both by `domain.PinName`. A blank name to `session.rename` is `bad_request`. The TUI's `R` opens a prompt on the status line, filled with the current name (`ctrl+u` clears it, `enter` pins, `esc` cancels, a blank name is not sent). `A` calls `session.unpin` when the task is pinned.
- The sidebar row truncates the name with an ellipsis at the pane width, keeping the harness tag. The full name is on the review's agent column (`SessionCard.Name`); the sidebar card that used to show it was removed (ADR 0014).

## Why

- Naming is a core rule, so it stays a pure function in `domain`; adapters only fetch titles.
- Resolving on a worker and merging into the current task keeps the loop free of network calls and means a slow Linear answer cannot undo a pin.
- The token sits in `config.toml` next to the theme so there is one user config file.

## Limits

- A pin belongs to the task, so every session on the same work item shares it.
- A task whose lookup failed, or that was created before the token was set, is not retried until a new session starts on a work item the daemon has never seen.
- The token is stored as plain text. Keep `config.toml` at `0600`.
- The 2 s budget for a Linear title is the API round trip; the tests check it against a mocked API.

## Rejected

- **A separate `linear.json`:** a second config file for one key.
- **Storing the name on the session:** sessions of one task would name themselves differently, and the sidebar already groups by task.
- **An LLM summary of the prompt:** out of scope for v1; the heuristic is free and instant.
