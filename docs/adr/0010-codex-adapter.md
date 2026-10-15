# ADR 0010: Codex adapter

Status: accepted, 2026-09-29.

## Decision

- `crew setup codex [--remove]` edits `$CODEX_HOME/hooks.json` (default `~/.codex`). It keeps every other hook and key in place, is a no-op when already applied, saves a timestamped backup before any change, and `--remove` undoes it. Tests only ever point it at a temp dir.
- The hook-name to event table stays in `domain.HookEvent`, so the daemon and the adapter share one mapping. `Interrupt` maps to `stop`. Subagent and compaction hooks are unmapped.
- Model, effort, context and limits come from the session's rollout file, read by a daemon worker off the event loop: the last 512 KiB, one read at a time per session, throttled to one per 2 s except on `Stop`, `SessionStart` and `UserPromptSubmit`. The hook payload's model is applied at once.
- Trust is left to the user. Codex keeps a hash per hook in `config.toml`; setup prints the step instead of writing hashes.

## Why

- The rollout has everything `/status` shows, needs no running process or credentials, and can be read after the fact. Reading it in a worker keeps the hook and the loop inside their budgets.
- A single mapping table cannot drift between the two callers.
- Writing trust hashes ourselves would bypass a check Codex put there for the user.

## Rejected

- **Codex's app-server API for limits:** needs a live connection and auth, for numbers the rollout already carries.
- **Scraping the status line or `/status` from the pane:** breaks on any layout change.
- **Rewriting `hooks.json` through a generic JSON map:** reorders the user's keys on the first run.
