# Codex adapter

## Hooks

`agentws setup codex` merges one group for each event into `$CODEX_HOME/hooks.json` (default `~/.codex`). Each group runs `agentws hook --harness codex --event <Name>`.

| Codex hook | Event | State |
|---|---|---|
| `SessionStart`, `SessionEnd` | `session_start`, `session_end` | `idle` |
| `UserPromptSubmit` | `user_prompt_submit` | `running` |
| `PreToolUse`, `PostToolUse` | `pre_tool_use`, `post_tool_use` | `running`, only while a turn is on |
| `PermissionRequest` | `permission_request` | `permission` |
| `Stop`, `Interrupt` | `stop` | `done` |

Not mapped: `SubagentStart`, `SubagentStop`, `PreCompact`, `PostCompact`. When a subagent stops, its parent must not become done. Codex has no hook for "waiting for input". Thus `waiting` never comes from Codex today.

Codex also has a legacy `notify` program that gets a JSON argument. `ParseNotify` maps `agent-turn-complete` to `stop`. Nothing installs it, because the hooks do the same work.

The hook stdin JSON has `session_id`, `transcript_path`, `cwd` and `model`. `transcript_path` is the rollout file below.

## Trust

Codex runs a hook only after the user trusts it. It records a hash for each hook in `config.toml` under `[hooks.state]`. Setup does not write those hashes. It prints the step: start codex and accept the review prompt, or use `/hooks`.

## Setup file handling

- Other hooks and unknown keys stay, in their order.
- A second run changes nothing and makes no backup.
- Before a run changes an existing file, it saves the file as `hooks.json.agentws-<UTC time>.bak`.
- `--remove` removes only our hooks. It deletes `hooks.json` if the file had nothing else in it.
- If `hooks.json` is not valid JSON, the command does not change it and fails.
- The command finds our hooks by the `hook --harness codex --event` in their command. Thus, if the binary moves, it writes the hook again and does not make a duplicate.

## Model, effort, context and limits

Only the model is in the hook payload. The other values come from the rollout file (`transcript_path`), one JSON line for each event:

- `turn_context`: `model` and `effort`.
- `event_msg` with `payload.type == "token_count"`:
  - `info.last_token_usage.total_tokens` and `info.model_context_window` give context.
  - `rate_limits.primary` and `rate_limits.secondary` give `used_percent`, `window_minutes` and `resets_at`. The plan sets which window is 5 h and which is weekly (`window_minutes` tells).

Context left is `100 * (window - 12000 - max(used - 12000, 0)) / (window - 12000)`, rounded. The 12000 is the baseline system prompt that Codex does not include in the number it shows. This follows Codex's own rule as I know it. Nobody has compared it with `/status` on a live session yet.

`Usage.LimitUsedPercent` is the highest window. After a hook, the daemon reads the last 512 KiB of the rollout in a worker, never on the event loop. It always reads on `Stop`, `SessionStart` and `UserPromptSubmit`, and at most every 2 s on other events.

Rate limits also show in these places, which the adapter does not use: the TUI `/status` and status line, and the app-server `account/rateLimits` API. The rollout needs no running process and no credentials.

## Panes

`ResolvePane` takes `$TMUX_PANE`. Without it, it goes up the process tree from a pid until it finds the process of a pane. `ProcessParent` reads a parent with `ps`. The walk is built and tested, but nothing calls it yet: the daemon needs the pid of the hook and the pid of each pane.

## Launch

`Launch` creates a pane that runs `codex --model <m> -c model_reasoning_effort="<e>" [-- <prompt>]` in the given directory, through the `TerminalHost` port.
