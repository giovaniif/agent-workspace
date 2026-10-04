# agent-workspace

`agentws` is a terminal workspace for running Claude Code and Codex sessions in parallel. Each session works in git worktrees, and the tool gives it a local, PR-style review pane. It is written in Go as one binary that acts as the daemon, the TUI, the CLI and the hook handler.

- What it does: [FEATURES.md](FEATURES.md)
- How it's built, the layers, and the performance budgets: [ARCHITECTURE.md](ARCHITECTURE.md)
- Decisions: [docs/adr/](docs/adr/)
- Work items: the GitHub issues, milestones `v1` (P0) and `v1.1` (P1). Each issue lists what it depends on; build those first.

## Scoped guidance

Claude Code and Codex load an `AGENTS.md` (here also `CLAUDE.md`, a symlink to it) from each directory they work in. Feature notes, commands and subsystem design live next to the code that owns them:

| Directory | Covers |
|---|---|
| [cmd/agentws/](cmd/agentws/AGENTS.md) | CLI subcommands, the hook process, startup cost, `agentws pr` JSON golden |
| [internal/domain/](internal/domain/AGENTS.md) | the pure rules (state machine, naming, cleanup, discovery, review, ports, disk), review prompt golden |
| [internal/app/](internal/app/AGENTS.md) | use cases and ports, cleanup execution, disk sizes |
| [internal/daemon/](internal/daemon/AGENTS.md) | process files, event loop, sessions, attention and banners, launcher, naming, fallback, worktree scan and PRs, ports, cleanup schedule, shell and nvim methods |
| [internal/rpc/](internal/rpc/AGENTS.md) | the socket protocol, methods, errors, build handshake, client |
| [internal/tui/](internal/tui/AGENTS.md) | sidebar, card, limits bar, theme and defaults, review viewer, disk view, goldens |
| [internal/adapters/](internal/adapters/AGENTS.md) | Claude and Codex adapters and their `setup`, notify, procs, fs, github, linear, sqlite |
| [internal/adapters/git/](internal/adapters/git/AGENTS.md) | git CLI use, repo facts, worktree listing, review diffs, turn snapshots, hunks |
| [internal/adapters/tmux/](internal/adapters/tmux/AGENTS.md) | the `agentws` tmux server, layout, shell panes, keys |
| [internal/adapters/setup/](internal/adapters/setup/AGENTS.md) | `.agentws.toml` setup recipes |
| [internal/adapters/onboard/](internal/adapters/onboard/AGENTS.md) | the first-run walkthrough and `agentws setup nvim` |
| [nvim/](nvim/AGENTS.md) | the Lua plugin |
| [scripts/](scripts/AGENTS.md) | `lint-comments`, `lint-agents`, `tdd-check`, `mutate`, `bench-hook.sh`, `dev`, `screenshot`, `web-screenshot`, `install.sh` |
| [web/](web/AGENTS.md) | the PWA: toolchain, `make web`, the service worker, pairing |
| [test/e2e/](test/e2e/AGENTS.md) | the core e2e suite and its fakes |
| [test/integration/](test/integration/AGENTS.md) | daemon-with-real-adapters integration tests |

**Where guidance goes.** New feature-specific guidance (a feature's test command, a by-hand recipe, a subsystem's design) goes in the `AGENTS.md` of the directory most edits for it start in, not here. A new scoped `AGENTS.md` gets a `CLAUDE.md` symlink beside it (`ln -s AGENTS.md CLAUDE.md`) and a row in the table above. This file stays under 150 lines; `scripts/lint-agents` (in `make lint`) enforces that, the symlinks and every relative link. See [ADR 0041](docs/adr/0041-scoped-agents-md.md).

## Commands

- `make build`: produces `./bin/agentws`.
- `make test`: `go test ./...`.
- `make web`: builds the PWA in `web/` into `internal/serve/dist` (needs Node 22.18+; see [web/](web/AGENTS.md)). `go build` works without it.
- `make lint`: `golangci-lint` (including the `depguard` layer rules and the `gofmt` formatter check in `.golangci.yml`), then `scripts/lint-comments` and `scripts/lint-agents`.
- `make bench`: benchmarks that guard the performance budgets.
- `make mutate`: `gremlins` on `domain` and `app`, failing below 80% efficacy (see [scripts/](scripts/AGENTS.md)).
- `make e2e`: the core e2e suite (about 5 s, needs `tmux`; see [test/e2e/](test/e2e/AGENTS.md)).
- `make dev [SEED=3] [FAKES=1]`: runs agentws from this checkout against its own home and tmux socket, rebuilding on change (see [scripts/](scripts/AGENTS.md)).
- `scripts/tdd-check <base> <head>`: what CI's `tdd` job runs (see [scripts/](scripts/AGENTS.md)).
- `make test`, `make bench` and `make e2e` pass `GO_TEST_FLAGS` (default `-p 2`) to keep local runs light.
- Integration tests use `-tags integration`. They need `git` and `tmux` installed, and use a temporary `AGENTWS_HOME`. The tmux ones also use a unique tmux socket each. Run them with `go test -p 2 -tags integration ./...`; CI does too, after `go vet -tags integration -composites=false ./...` so an integration-tagged file that stops compiling fails the build (`-composites=false` because chroma's positional rule tables trip the unkeyed-fields check). When a port changes, update the `-tags integration` fakes too (`*_fake_test.go`).
- Tests that start a daemon use a short `AGENTWS_HOME` under `/tmp`: macOS caps Unix socket paths at 104 bytes.
- By hand, run against a temporary `AGENTWS_HOME` and `AGENTWS_TMUX_SOCKET` (which points the daemon at another tmux server), so the real `~/.agentws` and `agentws` tmux server are untouched.
- `domain`, `app`, `tui`, `rpc` and `daemon` may not import `os/exec` (depguard). `internal/adapters/tmux` is the only code that runs tmux, and only `internal/daemon` may import it.

## Rules

- **Layers:** `domain` has no IO and no imports from other `internal/*` packages. `app` depends only on `domain` and defines the ports. Adapters implement those ports. `tui` talks only to `rpc`. Lint enforces this, so fix the design rather than the lint config.
- **Rules live in `domain`:** state transitions, naming and cleanup decisions go there as pure, table-tested functions, not in adapters or the TUI.
- **Nothing slow on hot paths:** no exec, disk or network calls in `agentws hook`, in TUI rendering, or in the daemon event loop. Heavy work goes to workers. Keep the budgets in ARCHITECTURE.md; if a change risks one, add or update a benchmark.
- **Shell out, don't reimplement:** use the `git`, `gh` and `tmux` CLIs. Never use a Go git library.
- **Never destroy user work:** cleanup code backs up uncommitted changes before removing anything, never deletes branches, and never touches a worktree that a process is using. Any change to cleanup needs tests for those cases.
- **Leave the user's setup alone:** the tmux adapter uses only the `agentws` tmux server. Setup commands merge into `~/.claude` and `~/.codex` config idempotently, back the file up first, and can be undone. Tests never touch the real `~/.claude`, `~/.codex` or `~/.config/nvim`.

## Tests: TDD, enforced by CI

- **Test first, always:** write the failing test, see it fail, then write the code. Never add a test after the behavior already exists.
- **Commit order proves it:** the test commit comes before the implementation commit. CI's `tdd` job fails a PR with any non-merge commit that changes both tests and production code (`refactor:` and `refactor(scope):` commits and Markdown files are exempt; see ADR 0043). It also runs the PR's new and changed tests against the base branch. They must fail there (a compile error counts as failing). A PR whose new tests pass on base fails the check. The check is per package: if any package with an added or changed `*_test.go` passes on base, the job fails, so keep test-only refactors in their own PR.
- **Test-only PRs titled `test:` are exempt:** deflakes and test refactors pass the `tdd` job when the title starts with `test:` and every changed file is a `*_test.go` file, under a `testdata/` dir, or under `test/`. If such a PR also touches any other file, the exemption is off, the job lists those files, and the normal rule applies. CI passes the title in `TDD_PR_TITLE`. See ADR 0033.
- **Protect the core concepts:** the domain state machine, naming, cleanup decisions, discovery, review scopes and the prompt format. Test through public behavior: inputs and outputs, not internals.
- **No useless tests:** no tests of getters, constructors, framework code or mocks calling mocks. CI runs mutation testing (`gremlins`) on `internal/domain` and `internal/app`; the mutation score must stay ≥ 80%. A test that kills no mutants gets deleted.
- **Fakes, not mocks:** unit tests use in-memory fakes of the ports. Integration tests use real temporary git repos and a real tmux server.
- **Port fakes live in `fakes_test.go`:** put in-memory fakes of ports in `fakes_test.go` (or `*_fake_test.go`) in their package. `scripts/tdd-check` skips those files, so adding a port method doesn't fail the `tdd` job. Behavior tests never go in them; they must still fail on base. See ADR 0009.
- **Small core e2e suite:** `test/e2e` covers the core flows only; keep it under 60 s. It is required in CI.

## Comments

- **No comments, anywhere.** Not in Go (tests included), shell, Lua, YAML, SQL, Makefiles or e2e `.txtar` scripts. No doc comments, no `why:`, no TODOs. Code explains itself through names and tests.
- A constraint or tool quirk the code cannot show goes into a test whose name states it, and into the commit message that introduced the workaround. Usage and design notes go in the scoped `AGENTS.md`, an ADR or the README. Open work goes in a GitHub issue.
- Only tool directives remain: `//go:` lines, `//line`, `//export`, a bare `//nolint:<linters>` (no reason after it), `// Code generated ... DO NOT EDIT.`, a shebang, `# shellcheck` and `# yaml-language-server:` lines.
- `scripts/lint-comments` fails `make lint` on any other comment. See ADR 0044.

## Working an issue

1. Read the issue, the FEATURES.md and ARCHITECTURE.md sections it links, the scoped `AGENTS.md` of the directories it touches, and the issues it depends on.
2. Make a branch named `<issue-number>-<slug>`.
3. For each behavior: commit a failing test, then commit the code that makes it pass, then refactor.
4. Meet every acceptance criterion. Run the commands in the issue's **Validate** section and paste their output into the PR body.
5. Open one PR per issue that says `Closes #<n>`. Title it with a conventional prefix (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`). Use `test:` only for a test-only change (no production code); it skips the "must fail on base" rule.
   A PR that changes what the TUI draws shows screenshots of the new behavior in its body. Make them with `scripts/screenshot` from a neutral demo folder (never a real home or project), push them to the `pr-assets` branch under `<issue-number>/`, and embed them by their `raw.githubusercontent.com` URL (see [scripts/](scripts/AGENTS.md)).
6. If a criterion turns out wrong or impossible, don't quietly drop it. Say so in the PR and on the issue.
7. main is protected: merge only via PR with build, tdd and mutate green and the branch up to date with main.
8. CodeRabbit reviews every PR and is a required check; fix or answer every finding and resolve all review threads before merging.

## Repo hygiene

- This repo is public. Never commit employer names, internal repo or service names, internal URLs, issue keys, tokens, or real usage data. Use generic examples such as `api`, `web`, `#42`.
