# agent-workspace

`agentws` is a terminal workspace for running Claude Code and Codex sessions in parallel. Each session works in git worktrees, and the tool gives it a local, PR-style review pane. It is written in Go as one binary that acts as the daemon, the TUI, the CLI and the hook handler.

- What it does: [FEATURES.md](FEATURES.md)
- How it's built, the layers, and the performance budgets: [ARCHITECTURE.md](ARCHITECTURE.md)
- Decisions: [docs/adr/](docs/adr/)
- Work items: the GitHub issues, milestones `v1` (P0) and `v1.1` (P1). Each issue lists what it depends on; build those first.

## Commands

- `make build`: produces `./bin/agentws`.
- `make test`: `go test ./...`.
- `make lint`: `golangci-lint` (including the `depguard` layer rules and the `gofmt` formatter check in `.golangci.yml`), then `scripts/lint-comments`. `scripts/tdd-check` ignores test changes that are whitespace only.
- `make bench`: benchmarks that guard the performance budgets.
- `make mutate`: `scripts/mutate` runs `gremlins` on `domain` and `app`, failing below 80% efficacy. A package with no tests is skipped. Install with `go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`.
- `make e2e`: the core e2e suite (about 5 s). It builds the binary once and runs the `test/e2e/testdata/script/*.txtar` scripts, each with its own temp `AGENTWS_HOME`, temp repos and `tmux -L` server, so it needs `tmux`. The fakes in `test/e2e/testdata/bin` are on `PATH`: `claude` and `codex` replay `$AGENTWS_E2E/<harness>.steps` (`hook`, `run`, `gate`, `read`; see the script header), and `gh` answers `gh api graphql` from `$AGENTWS_E2E/prs.json`. Scripts never sleep: `eventually <regexp> <cmd>` polls with a 15 s deadline, `capture VAR <regexp>` reads the last stdout, `repo <name>` makes a clone whose origin names github.com, and a fake waiting on `gate x` goes on after `mkdir $AGENTWS_E2E/gates/x`. The daemon reads two test hooks: `AGENTWS_TEST_CLOCK` (a duration such as `+2h`) moves the cleanup clock past the 1 h grace, and `AGENTWS_TEST_PR_POLL` shortens the PR poll. See [docs/adr/0032-core-e2e-suite.md](docs/adr/0032-core-e2e-suite.md).
- `scripts/tdd-check <base> <head>`: what CI's `tdd` job runs. It covers added or changed `*_test.go` files and files under a `testdata/` dir (such as e2e `.txtar` scripts), each counted for the package that owns them. It runs them with `-tags integration`, so integration tests count too. Run it locally on a clean tree to check a branch before pushing; it leaves you on a detached base checkout.
- `make test`, `make bench` and `make e2e` pass `GO_TEST_FLAGS` (default `-p 2`) to keep local runs light.
- Integration tests use `-tags integration`. They need `git` and `tmux` installed, and use a temporary `AGENTWS_HOME`. The tmux ones also use a unique tmux socket each. Run them with `go test -p 2 -tags integration ./...`; CI does too, after `go vet -tags integration -composites=false ./...` so an integration-tagged file that stops compiling fails the build (`-composites=false` because chroma's positional rule tables trip the unkeyed-fields check). When a port changes, update the `-tags integration` fakes too (`*_fake_test.go`). The git adapter's tests build real repos in temp dirs with `GIT_CONFIG_GLOBAL=/dev/null` so the user's git config never leaks in.
- `agentws daemon [start|status|stop]` runs or controls the daemon. Tests that start one use a short `AGENTWS_HOME` under `/tmp`: macOS caps Unix socket paths at 104 bytes.
- `scripts/bench-hook.sh [bin]`: times 200 `agentws hook` runs with the daemon up and down, prints p50/p95, and fails over budget (20 ms / 60 ms p95). CI runs it with `BUDGET_SCALE=2` because its runners start processes about 3x slower. Keep `cmd/agentws` startup light: package init costs every hook, and `modernc.org/sqlite` init is already about 4.5 of the 5 ms locally.
- `agentws workspace add <path>|list|remove <path>` registers workspaces through the daemon (auto-started). `list` shows `-` for a repo's git facts until the first background refresh lands.
- `agentws` attaches to the client layout (creating it and the daemon if needed); `agentws tui` is what runs in its left pane. `agentws debug seed N` adds N fake sessions, with event logs for the session card and fresh limits for both harnesses (one per-model window is low, so the red state shows), for trying the TUI. The first seeded session also gets three subagents, one nested, two running. Its PR is open with failing checks, so the card shows a PR board (`agentws pr seed-session-1`).
- `AGENTWS_TMUX_SOCKET` points the daemon at another tmux server. Use it with a temporary `AGENTWS_HOME` when running the TUI by hand, so the real `agentws` server is untouched.
- TUI goldens live in `internal/tui/testdata/*.golden`; regenerate with `go test ./internal/tui/ -run Golden -update` and review the diff.
- `agentws debug session [--once] <id>` prints a session's state, harness, pane, model, effort, context left and limit used, then each change to it until interrupted (`--once` prints just the current line).
- `agentws setup codex [--remove]` merges (or removes) the agentws hooks in `$CODEX_HOME/hooks.json`. Tests of it, and of anything else that touches Codex config, use a temp `CODEX_HOME`; never point them at the real `~/.codex`.
- `agentws setup claude [--remove]` merges agentws hooks and the status-line wrapper into `$CLAUDE_CONFIG_DIR/settings.json` (default `~/.claude`). Tests set `CLAUDE_CONFIG_DIR` to a temp dir and never touch the real `~/.claude`.
- In the TUI, `M` and `E` switch the selected session's model and effort. The daemon types `/model <x>` or `/effort <y>` into the pane once the agent is between tools. Tests fake the terminal host; by hand, run the TUI against a fake `claude` on `PATH` with a temporary `AGENTWS_HOME` and `AGENTWS_TMUX_SOCKET`. See [docs/adr/0018-model-effort-switching.md](docs/adr/0018-model-effort-switching.md).
- `agentws setup-worktree <path>` applies the `[setup]` recipe in the main checkout's `.agentws.toml` (`copy`, `link`, `run`, `deps = clone|link|install`) to a linked worktree, in-process (no daemon). Its integration tests are named `TestRecipe*`: `go test -tags integration -run Recipe ./internal/adapters/setup`. See [docs/adr/0013-setup-recipes.md](docs/adr/0013-setup-recipes.md).
- Banners: `go test ./... -run Notify` runs every notification test. To try them by hand, put a stub `osascript` first on `PATH` for the daemon (it logs its argv), or fire at most a couple of real ones; use a temp `AGENTWS_HOME` and `AGENTWS_TMUX_SOCKET`. Sounds come from `$AGENTWS_HOME/notify.json`. In the TUI, `m` mutes the selected session.
- Codex fallback: `go test ./... -run Fallback`. `[fallback]` in `$AGENTWS_HOME/config.toml` sets `threshold` and the `models`/`efforts` maps from Claude to Codex; in the new-session dialog `ctrl+s` takes the offer. See [docs/adr/0027-codex-fallback.md](docs/adr/0027-codex-fallback.md).
- Launcher: `go test ./... -run Launcher -tags integration` runs the domain, daemon and TUI launcher tests and the integration test (real repos and tmux, a fake Linear server; nothing reaches Linear). `[launcher] max_parallel` in `$AGENTWS_HOME/config.toml` sets the limit (default 3). In the TUI `L` opens the input, `c` moves queued issues to Codex when offered, `X` clears the queue. See [docs/adr/0031-linear-launcher.md](docs/adr/0031-linear-launcher.md).
- Naming: `go test ./... -run Naming` runs every naming test (domain rules, resolvers, the Linear and `gh` adapters against a mocked API and a fake `gh`, the daemon and the TUI). Real Linear lookups need `[linear] token = "..."` in `$AGENTWS_HOME/config.toml`; tests never read it. In the TUI, `R` renames and pins, `A` unpins. See [docs/adr/0026-session-naming.md](docs/adr/0026-session-naming.md).
- `agentws worktree list|assign <path> <session>` shows detected worktrees and sets their owner.
- Ports: `go test ./... -run Ports -tags integration -bench Ports` runs the adapter, daemon, TUI and end-to-end ports tests plus `BenchmarkPortsRefresh` (fails above 50 ms). Tests start their own throwaway servers and only signal process groups they started. In the TUI, `K` kills the selected session's dev servers after `y`. See [docs/adr/0022-ports-view.md](docs/adr/0022-ports-view.md).
- Review: `go test ./internal/... -run Review -tags integration -bench Review` runs every review test and both review benchmarks. `r` in the TUI opens it. Turn snapshots live under `refs/agentws/turns/`. New syntax lexers go in `internal/tui/syntax/` as chroma XML files; depguard bans chroma's `lexers` and `styles` packages (their init costs every hook). See [docs/adr/0023-review-pane.md](docs/adr/0023-review-pane.md).
- `agentws cleanup [--dry-run]` runs (or only prints) the cleanup plan through the daemon. Actions go to `$AGENTWS_HOME/cleanup.log`, backups to `$AGENTWS_HOME/backups/`, and removed worktrees to `$AGENTWS_HOME/trash/`. Its tests are named `*Cleanup*`: `go test ./internal/domain/... -run Cleanup` and `go test -tags integration -run CleanupExec ./...`. Only ever run cleanup against temp repos with a temp `AGENTWS_HOME`; never point it at a real checkout while testing. See [docs/adr/0021-worktree-cleanup.md](docs/adr/0021-worktree-cleanup.md).
- `agentws pr <session id or name> [--json]` prints a session's PR board from daemon state. The JSON shape is pinned by `cmd/agentws/testdata/pr.json.golden` (regenerate with `go test ./cmd/agentws -run PRBoardJSON -update` and review the diff) and documented in [docs/adr/0024-pr-board.md](docs/adr/0024-pr-board.md). The github adapter's tests and the integration tests use a fake `gh` script; nothing reaches GitHub. Run the board tests with `go test ./... -run PRBoard`.
- `test/integration` holds `-tags integration` tests that run the daemon with its real adapters (the daemon package itself may not exec). CI runs them with every other integration-tagged package.
- Worktrees and disk view: `go test ./internal/tui/... -run Worktrees`, plus `-run 'Disk|Reclaimable|TotalSize|RemoveWorktree|CleanupWorktree|WorktreeShell'` in `internal/daemon`, `internal/app` and `internal/domain`, and `go test -tags integration -run Disk ./internal/adapters/fs/` for `du`. `w` opens it. `AGENTWS_DEPS_STORE` names the shared deps store whose size the header shows (default: pnpm's store if present). By hand, run it against temp repos with a temp `AGENTWS_HOME` and `AGENTWS_TMUX_SOCKET`: its `d` and `b` really remove worktrees. See [docs/adr/0030-worktrees-disk-view.md](docs/adr/0030-worktrees-disk-view.md).
- Shell and nvim: `go test ./... -run Shell -tags integration` runs the shell tests (daemon against real tmux, the tmux adapter's split, popup and key pass-through). They and the nvim ones need `tmux` and `nvim`; the nvim-only tests skip without it, and CI installs it. `TestNvimPluginSpecs` (`test/integration`) runs `nvim/test/spec.lua` with `nvim --clean` and throwaway XDG dirs: never point plugin tests at `~/.config/nvim`. To try it by hand, use a temporary `AGENTWS_HOME`, `AGENTWS_TMUX_SOCKET` and `XDG_CONFIG_HOME` whose `nvim/init.lua` prepends `nvim/` to the runtimepath and calls `require('agentws').setup({})`. See [docs/adr/0029-shell-and-nvim.md](docs/adr/0029-shell-and-nvim.md).
- `agentws review comment [--session id] (--file abs | --worktree id --path rel) --start n [--end n] [--code text] --body text` adds a draft comment; `agentws review scope [--session id] [--scope s]` prints each worktree's path, base commit and files as JSON. `agentws review send [--session id]` sends the draft, or queues it until the agent is between tools. `--session` defaults to `$AGENTWS_SESSION`, which the shell and nvim panes carry.
- Review comments: `go test ./... -run ReviewSend -tags integration` runs the send tests, including one that pastes into a real tmux pane. The prompt format is pinned by `internal/domain/testdata/review_prompt.golden`; change it only on purpose and update the golden by hand. Hunk tests (`-run 'ReviewStage|ReviewRevert'`, `adapters/git`) compare against `git add -p` and `git checkout -p` in temp repos. Reverted hunks are backed up under `<git dir>/agentws/reverted/`. See [docs/adr/0028-review-comments-and-hunks.md](docs/adr/0028-review-comments-and-hunks.md).
- `agentws new [--workspace p] [--harness claude|codex] [--model m] [--effort e] <work item>` starts a session like the TUI's `n` dialog; without `--workspace` it uses the last used one. New single-repo worktrees go under `$AGENTWS_HOME/worktrees`.
- `agentws debug launch --harness claude --dir <dir> [--model m] [--effort e]` opens a harness pane in any dir, with no task or worktree.
- The daemon's `NewSession` integration test (`go test -tags integration -run NewSession ./internal/daemon/`) uses real temp repos, a fake harness script and its own tmux socket.
- `domain`, `app`, `tui`, `rpc` and `daemon` may not import `os/exec` (depguard). `internal/adapters/tmux` is the only code that runs tmux, and only `internal/daemon` may import it.

## Rules

- **Layers:** `domain` has no IO and no imports from other `internal/*` packages. `app` depends only on `domain` and defines the ports. Adapters implement those ports. `tui` talks only to `rpc`. Lint enforces this, so fix the design rather than the lint config.
- **Rules live in `domain`:** state transitions, naming and cleanup decisions go there as pure, table-tested functions, not in adapters or the TUI.
- **Nothing slow on hot paths:** no exec, disk or network calls in `agentws hook`, in TUI rendering, or in the daemon event loop. Heavy work goes to workers. Keep the budgets in ARCHITECTURE.md; if a change risks one, add or update a benchmark.
- **Shell out, don't reimplement:** use the `git`, `gh` and `tmux` CLIs. Never use a Go git library.
- **Never destroy user work:** cleanup code backs up uncommitted changes before removing anything, never deletes branches, and never touches a worktree that a process is using. Any change to cleanup needs tests for those cases.
- **Leave the user's setup alone:** the tmux adapter uses only the `agentws` tmux server. Setup commands merge into `~/.claude` and `~/.codex` config idempotently, back the file up first, and can be undone.

## Tests: TDD, enforced by CI

- **Test first, always:** write the failing test, see it fail, then write the code. Never add a test after the behavior already exists.
- **Commit order proves it:** the test commit comes before the implementation commit. CI's `tdd` job runs the PR's new and changed tests against the base branch. They must fail there (a compile error counts as failing). A PR whose new tests pass on base fails the check. The check is per package: if any package with an added or changed `*_test.go` passes on base, the job fails, so keep test-only refactors in their own PR.
- **Test-only PRs titled `test:` are exempt:** deflakes and test refactors pass the `tdd` job when the title starts with `test:` and every changed file is a `*_test.go` file, under a `testdata/` dir, or under `test/`. If such a PR also touches any other file, the exemption is off, the job lists those files, and the normal rule applies. CI passes the title in `TDD_PR_TITLE`. See ADR 0033.
- **Protect the core concepts:** the domain state machine, naming, cleanup decisions, discovery, review scopes and the prompt format. Test through public behavior: inputs and outputs, not internals.
- **No useless tests:** no tests of getters, constructors, framework code or mocks calling mocks. CI runs mutation testing (`gremlins`) on `internal/domain` and `internal/app`; the mutation score must stay ≥ 80%. A test that kills no mutants gets deleted.
- **Fakes, not mocks:** unit tests use in-memory fakes of the ports. Integration tests use real temporary git repos and a real tmux server.
- **Port fakes live in `fakes_test.go`:** put in-memory fakes of ports in `fakes_test.go` (or `*_fake_test.go`) in their package. `scripts/tdd-check` skips those files, so adding a port method doesn't fail the `tdd` job. Behavior tests never go in them; they must still fail on base. See ADR 0009.
- **Domain tests are in-package** (`package domain`): depguard bans `domain` from importing `internal/...`, and that includes an external `domain_test` package importing `domain`.
- **Small core e2e suite:** `test/e2e` uses `testscript` with fake `claude`, `codex` and `gh` binaries. It covers: start a session → hook events → state; an agent-created worktree gets attached; a merged PR gets its worktree cleaned; review comments get sent. Keep it small and fast (< 60 s). It is required in CI.

## Comments

- Code explains itself through names. A comment only says *why*, never *what*.
- Inside function bodies, the only comments allowed are ones starting with `// why:` and tool directives (`//go:`, `//nolint:` with a reason). `scripts/lint-comments` enforces this in CI.
- Doc comments only where they add information a caller needs. No boilerplate docs that restate the name.
- `scripts/lint-comments` rejects a declaration doc that starts with the name and says nothing else: once filler words (returns, is, a, the, new, creates...) and the words of the name, receiver, params and types are dropped, nothing is left (`// Close closes the store.`). It also rejects section banners (`// ---- helpers ----`, a lone `// Helpers` between declarations). See ADR 0020.
- A TODO must reference an issue: `// TODO(#12): ...`.
- A `//nolint:` directive needs a reason after it: `//nolint:gosec // why: ...`.
- `scripts/lint-comments` fixtures live in its `testdata/` as `*.go.txt` so no other tool compiles them.

## Working an issue

1. Read the issue, the FEATURES.md and ARCHITECTURE.md sections it links, and the issues it depends on.
2. Make a branch named `<issue-number>-<slug>`.
3. For each behavior: commit a failing test, then commit the code that makes it pass, then refactor.
4. Meet every acceptance criterion. Run the commands in the issue's **Validate** section and paste their output into the PR body.
5. Open one PR per issue that says `Closes #<n>`. Title it with a conventional prefix (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`). Use `test:` only for a test-only change (no production code); it skips the "must fail on base" rule.
6. If a criterion turns out wrong or impossible, don't quietly drop it. Say so in the PR and on the issue.
7. main is protected: merge only via PR with build, tdd and mutate green and the branch up to date with main.
8. CodeRabbit reviews every PR and is a required check; fix or answer every finding and resolve all review threads before merging.

## Repo hygiene

- This repo is public. Never commit employer names, internal repo or service names, internal URLs, issue keys, tokens, or real usage data. Use generic examples such as `api`, `web`, `#42`.
- Commits use the GitHub noreply email already set in this clone's git config.
