# ADR 0006: Hook ingestion

Status: accepted, 2026-09-29.

## Decision

- `crew hook --harness claude|codex --event <name>` reads the hook JSON from stdin, adds `$TMUX_PANE` and a timestamp, and sends one `hook` request on the socket. It always exits 0 and logs failures to `$CREW_HOME/hook.log`.
- It dials the socket directly with a 50 ms deadline for the whole exchange, instead of `rpc.Client` or `rpc.Connect`. It never starts the daemon.
- Fire-and-forget: it closes right after writing, except for `UserPromptSubmit`, where it reads the `HookReply` within the same deadline and prints its `output`.
- The daemon owns the mapping: pane to session (`Session.Pane`) and hook name to harness event, both pure functions in `domain`.
- `scripts/bench-hook.sh` measures wall time over 200 runs. CI runs it with budgets doubled (`BUDGET_SCALE=2`): its macOS runners measured p50 15 ms, p95 22 ms against 5 ms locally, with the daemon down as slow as up, so the gap is process start, not the socket.

## Why

- The agent blocks on every hook. Auto-starting the daemon, or waiting for a reply nobody reads, would put the daemon's latency on the agent's hot path.
- Mapping in the daemon keeps the hook process free of state lookups, so it stays at process-start cost (about 5 ms on an M-series Mac). Most of that is `modernc.org/sqlite` package init (`GODEBUG=inittrace=1`: about 4.5 ms), which every subcommand pays because it is one binary.

## Rejected

- **A separate small hook binary:** the one-binary rule stays; startup is already well under budget.
