# test/e2e

The core e2e suite (`make e2e`, about 5 s). CI requires it. See [ADR 0032](../../docs/adr/0032-core-e2e-suite.md).

- **Scope.** `testscript` with fake `claude`, `codex` and `gh` binaries. It covers these flows:
  - Start a session → hook events → state.
  - A worktree that an agent creates gets attached.
  - A merged PR gets its worktree cleaned.
  - Review comments get sent.

  Keep it small and fast (< 60 s).
- **How it runs.** It builds the binary one time and runs the `testdata/script/*.txtar` scripts. Each script has its own temp `AGENTWS_HOME`, temp repos and `tmux -L` server. Thus it needs `tmux`.
- **Fakes.** `testdata/bin` is on `PATH`. `claude` and `codex` replay `$AGENTWS_E2E/<harness>.steps`, one line at a time:
  - `hook <Event> [json]` pipes the json (default `{}`) to `agentws hook`.
  - `run <shell>` runs a command as the Bash tool, between PreToolUse and PostToolUse.
  - `gate <name>` blocks until `$AGENTWS_E2E/gates/<name>` exists (30 s limit, two times the 15 s polling of the scripts).
  - `read <file>` saves the text that is typed into the pane.

  After the last step, the fake stays alive and holds its pane. `agentws hook` drops an event that it cannot send in 50 ms. A loaded runner can cause this. Thus the fake sends an unsent event again, for a maximum of 5 tries. A late reply means that the event was sent, so the fake does not try again. `gh` answers `gh api graphql` from `$AGENTWS_E2E/prs.json`. Stub `osascript` and `terminal-notifier` log their argv. `make dev FAKES=1` puts the fake harnesses on `PATH` for manual runs.
- **No sleeps.**
  - `eventually <regexp> <cmd>` polls with a 15 s deadline.
  - `capture VAR <regexp>` reads the last stdout.
  - `repo <name>` makes a clone whose origin names github.com.
  - A fake that waits on `gate x` continues after `mkdir $AGENTWS_E2E/gates/x`.
- **Daemon test hooks.** `AGENTWS_TEST_CLOCK` (a duration such as `+5h`) moves the cleanup clock past the 4 h grace. `AGENTWS_TEST_PR_POLL` makes the PR poll shorter.
