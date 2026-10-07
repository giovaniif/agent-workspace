# agent-workspace

`agentws` is a terminal workspace that runs Claude Code and Codex sessions in parallel. Each session works in git worktrees. The tool gives each session a local, PR-style review pane. `agentws` is one Go binary. It is the daemon, the TUI, the CLI and the hook handler.

- What it does: [FEATURES.md](FEATURES.md)
- How it is built, the layers and the performance budgets: [ARCHITECTURE.md](ARCHITECTURE.md)
- Decisions: [docs/adr/](docs/adr/)
- Work items: the GitHub issues, milestones `v1` (P0) and `v1.1` (P1). Each issue lists the issues it depends on. Build those first.
- Terms and writing style: [docs/style.md](docs/style.md)

## Scoped guidance

Claude Code and Codex load an `AGENTS.md` from each directory they work in. Here, `CLAUDE.md` is a symlink to it. Feature notes, commands and subsystem design are next to the code that owns them:

| Directory | Covers |
|---|---|
| [cmd/agentws/](cmd/agentws/AGENTS.md) | CLI subcommands, the hook process, startup cost, `agentws pr` JSON golden |
| [internal/domain/](internal/domain/AGENTS.md) | the pure rules (state machine, naming, cleanup, discovery, review, ports, disk), review prompt golden |
| [internal/app/](internal/app/AGENTS.md) | use cases and ports, cleanup execution, disk sizes |
| [internal/daemon/](internal/daemon/AGENTS.md) | process files, event loop, sessions, attention and banners, launcher, naming, fallback, worktree scan and PRs, ports, cleanup schedule, shell and nvim methods |
| [internal/rpc/](internal/rpc/AGENTS.md) | the socket protocol, methods, errors, build handshake, client |
| [internal/serve/](internal/serve/AGENTS.md) | `agentws serve`: the HTTP and WebSocket API for the phone app, its routes, auth, stream frames, TLS |
| [internal/view/](internal/view/AGENTS.md) | the derived view shared by `serve` and `view.subscribe`: names, banners, order, PR board, goldens |
| [internal/syntax/](internal/syntax/AGENTS.md) | the curated chroma lexers the TUI and `agentws rpc` share, review spans |
| [internal/tui/](internal/tui/AGENTS.md) | sidebar, limits bar, theme and defaults, review viewer, disk view, goldens |
| [internal/adapters/](internal/adapters/AGENTS.md) | Claude and Codex adapters and their `setup`, notify, procs, fs, github, linear, sqlite |
| [internal/adapters/git/](internal/adapters/git/AGENTS.md) | git CLI use, repo facts, worktree listing, review diffs, turn snapshots, hunks |
| [internal/adapters/tmux/](internal/adapters/tmux/AGENTS.md) | the `agentws` tmux server, layout, shell panes, keys |
| [internal/adapters/setup/](internal/adapters/setup/AGENTS.md) | `.agentws.toml` setup recipes |
| [internal/adapters/onboard/](internal/adapters/onboard/AGENTS.md) | the first-run walkthrough and `agentws setup nvim` |
| [nvim/](nvim/AGENTS.md) | the Lua plugin |
| [scripts/](scripts/AGENTS.md) | `lint-comments`, `lint-agents`, `tdd-check`, `mutate`, `bench-hook.sh`, `dev`, `screenshot`, `web-screenshot`, `install.sh` |
| [web/](web/AGENTS.md) | the PWA: toolchain, `make web`, the service worker, pairing |
| [macos/](macos/AGENTS.md) | the Mac app: the `AgentwsKit` package, the app target, `swift test`, `build-app` |
| [test/e2e/](test/e2e/AGENTS.md) | the core e2e suite and its fakes |
| [test/integration/](test/integration/AGENTS.md) | daemon-with-real-adapters integration tests |

**Where guidance goes.** Put new feature-specific guidance in the `AGENTS.md` of the directory where most edits for that feature start, not here. Feature-specific guidance is a feature's test command, a by-hand recipe or a subsystem's design. Give a new scoped `AGENTS.md` a `CLAUDE.md` symlink beside it (`ln -s AGENTS.md CLAUDE.md`) and a row in the table above. Keep this file under 150 lines. `scripts/lint-agents` (in `make lint`) checks the line count, the symlinks and every relative link. See [ADR 0041](docs/adr/0041-scoped-agents-md.md).

## Writing docs

Write all prose docs and new ADRs in ASD-STE100 Simplified Technical English. Use only the approved terms of the glossary. The rules and the glossary are in [docs/style.md](docs/style.md). When you edit prose, do not change code, commands, paths or identifiers.

## Commands

- `make build`: makes `./bin/agentws`.
- `make test`: `go test ./...`.
- `make web`: builds the PWA in `web/` into `internal/serve/dist`. It needs Node 22.18+ (see [web/](web/AGENTS.md)). `go build` works without it.
- `make lint`: `golangci-lint` (with the `depguard` layer rules and the `gofmt` formatter check in `.golangci.yml`), then `scripts/lint-comments` and `scripts/lint-agents`.
- `make bench`: benchmarks that guard the performance budgets.
- `make mutate`: `gremlins` on `domain` and `app`. It fails below 80% efficacy (see [scripts/](scripts/AGENTS.md)).
- `make e2e`: the core e2e suite. It takes about 5 s and needs `tmux` (see [test/e2e/](test/e2e/AGENTS.md)).
- `make dev [SEED=3] [FAKES=1]`: runs agentws from this checkout with its own home and tmux socket. It builds again on each change (see [scripts/](scripts/AGENTS.md)).
- `scripts/tdd-check <base> <head>`: the check that CI's `tdd` job runs (see [scripts/](scripts/AGENTS.md)).
- `make test`, `make bench` and `make e2e` pass `GO_TEST_FLAGS` (default `-p 2`). This keeps local runs light.
- Integration tests use `-tags integration`. They need `git` and `tmux`, and they use a temporary `AGENTWS_HOME`. Each tmux test also uses its own tmux socket.
  - Run them with `go test -p 2 -tags integration ./...`. CI runs them the same way.
  - Before the tests, CI runs `go vet -tags integration -composites=false ./...`. Thus an integration-tagged file that does not compile fails the build. `-composites=false` is necessary because chroma's positional rule tables fail the unkeyed-fields check.
  - When a port changes, update the `-tags integration` fakes too (`*_fake_test.go`).
- Tests that start a daemon use a short `AGENTWS_HOME` under `/tmp`, because macOS limits Unix socket paths to 104 bytes.
- For runs by hand, use a temporary `AGENTWS_HOME` and `AGENTWS_TMUX_SOCKET`. `AGENTWS_TMUX_SOCKET` points the daemon at another tmux server. Thus the real `~/.agentws` and the `agentws` tmux server do not change.
- `domain`, `app`, `tui`, `rpc` and `daemon` must not import `os/exec` (depguard). Only `internal/adapters/tmux` runs tmux, and only `internal/daemon` can import it.

## Rules

- **Layers:** `domain` has no IO and no imports from other `internal/*` packages. `app` depends only on `domain` and defines the ports. Adapters implement those ports. `tui` talks only to `rpc`. Lint enforces this. If lint fails, fix the design, not the lint config.
- **Rules live in `domain`:** put state transitions, naming and cleanup decisions in `domain` as pure, table-tested functions. Do not put them in adapters or the TUI.
- **Nothing slow on hot paths:** do not use exec, disk or network calls in `agentws hook`, in TUI rendering or in the daemon event loop. Send heavy work to workers. Keep the budgets in ARCHITECTURE.md. If a change can break a budget, add or update a benchmark.
- **Shell out, do not reimplement:** use the `git`, `gh` and `tmux` CLIs. Do not use a Go git library.
- **Never destroy user work:** cleanup code backs up uncommitted changes before it removes anything. It never deletes branches. It never touches a worktree that a process uses. Each change to cleanup needs tests for these cases.
- **Do not change the user's setup:** the tmux adapter uses only the `agentws` tmux server. Setup commands merge into the `~/.claude` and `~/.codex` config idempotently. They back up the file first, and you can undo them. Tests never touch the real `~/.claude`, `~/.codex` or `~/.config/nvim`.

## Tests: TDD, enforced by CI

- **Test first, always:** write the failing test, see it fail, then write the code. Do not add a test after the behavior exists.
- **Commit order proves it:** the test commit comes before the implementation commit.
  - CI's `tdd` job fails a PR if a non-merge commit changes both tests and production code. `refactor:` and `refactor(scope):` commits and Markdown files are exempt (see ADR 0043).
  - The job also runs the PR's new and changed tests against the base branch. They must fail there. A compile error counts as a failure. If the new tests of a PR pass on base, the check fails.
  - The check is per package. If a package with an added or changed `*_test.go` passes on base, the job fails. Thus, keep test-only refactors in their own PR.
- **Test-only PRs titled `test:` are exempt:** deflakes and test refactors pass the `tdd` job when the title starts with `test:` and each changed file is a `*_test.go` file, or is under a `testdata/` dir or under `test/`. If such a PR also touches a different file, the exemption is off. The job then lists those files and applies the normal rule. CI passes the title in `TDD_PR_TITLE`. See ADR 0033.
- **Protect the core concepts:** the domain state machine, naming, cleanup decisions, discovery, review scopes and the prompt format. Test through public behavior: inputs and outputs, not internals.
- **No useless tests:** do not test getters, constructors, framework code or mocks that call mocks. CI runs mutation testing (`gremlins`) on `internal/domain` and `internal/app`. The mutation score must stay ≥ 80%. Delete a test that kills no mutants.
- **Fakes, not mocks:** unit tests use in-memory fakes of the ports. Integration tests use real temporary git repos and a real tmux server.
- **Port fakes live in `fakes_test.go`:** put in-memory fakes of ports in `fakes_test.go` (or `*_fake_test.go`) in their package. `scripts/tdd-check` skips those files, so a new port method does not fail the `tdd` job. Do not put behavior tests in them. Behavior tests must still fail on base. See ADR 0009.
- **Small core e2e suite:** `test/e2e` covers only the core flows. Keep it under 60 s. CI requires it.

## Comments

- **No comments, anywhere.** This applies to Go (tests too), shell, Lua, YAML, SQL, Makefiles and e2e `.txtar` scripts. Do not write doc comments, `why:` or TODOs. Names and tests explain the code.
- Put a constraint or tool quirk that the code cannot show into a test whose name states it. Also put it into the commit message of the workaround. Put usage and design notes in the scoped `AGENTS.md`, an ADR or the README. Put open work in a GitHub issue.
- Only these tool directives stay: `//go:` lines, `//line`, `//export`, a bare `//nolint:<linters>` (with no reason after it), `// Code generated ... DO NOT EDIT.`, a shebang, `# shellcheck` and `# yaml-language-server:` lines.
- `scripts/lint-comments` fails `make lint` on all other comments. See ADR 0044.

## Working an issue

1. Read the issue, the FEATURES.md and ARCHITECTURE.md sections it links, the scoped `AGENTS.md` of the directories it touches, and the issues it depends on.
2. Make a branch named `<issue-number>-<slug>`.
3. For each behavior: commit a failing test, then commit the code that makes it pass, then refactor.
4. Meet each acceptance criterion. Run the commands in the issue's **Validate** section and paste their output into the PR body.
5. Open one PR per issue with `Closes #<n>` in it. Start its title with a conventional prefix (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`). Use `test:` only for a test-only change (no production code). It skips the "must fail on base" rule.
   If a PR changes what the TUI draws, show screenshots of the new behavior in its body. Make them with `scripts/screenshot` from a neutral demo folder, never a real home or project. Push them to the `pr-assets` branch under `<issue-number>/`. Embed them by their `raw.githubusercontent.com` URL (see [scripts/](scripts/AGENTS.md)).
6. If a criterion is wrong or impossible, do not quietly drop it. Say so in the PR and on the issue.
7. main is protected. Merge only through a PR with build, tdd and mutate green and the branch up to date with main.
8. CodeRabbit reviews each PR and is a required check. Before you merge, fix or answer each finding and resolve all review threads.

## Repo hygiene

- This repo is public. Never commit employer names, internal repo or service names, internal URLs, issue keys, tokens or real usage data. Use generic examples such as `api`, `web`, `#42`.
