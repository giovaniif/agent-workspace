# ADR 0050: Automations

Status: accepted, 2026-10-13. Epic #330. Issues #331, #332, #333, #334 and #335 implement it.

## Context

The owner starts the same kinds of sessions again and again: "fix the red CI of this PR", "review the PRs that ask for my review", "update the dependencies each Monday". Each time, the owner types the same prompt and selects the same harness, model, effort and target.

Orca has automations: a saved prompt with an agent, a target, an optional schedule and optional triggers. We copy that model. We add one thing: a run by hand can add text to the prompt at run time (the context).

What exists:

- `session.new` starts a session from a workspace, a work item, a harness, a model, an effort and a first prompt.
- `$AGENTWS_HOME/config.toml` holds global settings. A repo's `.agentws.toml` holds its setup recipe (ADR 0013).
- The daemon already sees PR changes (ADR 0024), session state changes (ADR 0015) and new worktrees.
- The store is sqlite (ADR 0004).

## Decision

### An automation is a saved pre-prompt

An automation has these fields:

| Field | Meaning |
|---|---|
| `name` | The key. It is unique in one scope. |
| `agent` | The harness: `claude`, `codex` or `omp`. |
| `model`, `effort` | Optional. Empty values use `[defaults.<harness>]`. |
| `target` | `new-worktree`, `worktree:<path>` or `workspace`. |
| `prompt` | The prompt template. |
| `schedule` | Optional. A preset or a cron expression (#333). |
| `on` | Optional. A list of event names (#334). |
| `precheck` | Optional. A shell command. A non-zero exit skips the run (#332). |
| `session` | `fresh` (the default) or `reuse`. |
| `enabled` | The default is true. |

### Where automations are defined

- Global automations are `[[automation]]` tables in `~/.agentws/config.toml` (`$AGENTWS_HOME/config.toml`).
- Repo automations are `[[automation]]` tables in the repo's `.agentws.toml`. They apply only to that repo.
- A repo automation with the same name as a global one replaces the global one for that repo. The merge does not combine fields.
- The validation, the merge and the template expansion are pure functions in `domain`, with table tests.
- We do not add a new file or a new store for definitions. The two TOML files are the only source. Users can put them in their dotfiles and in the repo.

### The prompt template

- The variables are `{{context}}`, `{{work_item}}`, `{{repo}}`, `{{branch}}` and `{{event}}`.
- If the template has no `{{context}}` and the run has context, the expansion appends the context at the end, after one blank line.
- An unknown variable is a validation error. Thus a typing error shows when the definition loads, not at run time.

### Each run is a normal session

- A run calls the same code as `session.new`. Thus a run has a worktree, a review, attention, banners and history.
- The daemon records each run in sqlite: the automation, the trigger, the context, the start, the end, the outcome and the session ID.
- The triggers in v1 are: by hand with context, schedule and events.

### Methods

- `automation.list` and `automation.show` (#331), `automation.run` and `automation.runs` (#332).
- The CLI is `agentws automation list|show|run|runs`. The Mac app, the TUI and the PWA use the same methods.

## Consequences

- The definitions are text in files that the user already has. A client that edits an automation writes to `config.toml` with the backup and atomic rename of `config.set`. A client does not edit a repo's `.agentws.toml`. It shows where that file is.
- A schedule or an event can start sessions when the owner is away. The launcher limit (`launcher.max_parallel`) still applies, and a run never starts while the last run of the same automation is live.
- The browser of epic #330 is a different decision. It gets its own ADR.

## Alternatives

- **Definitions in sqlite, edited only through the apps.** Rejected. The owner cannot keep them in dotfiles or in a repo, and a review cannot show a change to them.
- **A field-by-field merge of repo and global definitions.** Rejected. It is hard to see which value applies. A full replacement is easy to explain.
