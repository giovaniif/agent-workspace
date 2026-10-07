# scripts

Repo tooling. The repo-wide rules are in the [root AGENTS.md](../AGENTS.md).

- **`lint-comments [dir...]`** (`make lint`): fails on any comment in these files:
  - Go, shell (`.sh` or a shell shebang), Makefiles, YAML, Lua and SQL.
  - `.txtar` scripts (only the script part, before the first `-- file --`).
  - TypeScript and JavaScript (`.ts`, `.tsx`, `.mts`, `.cts`, `.js`, `.jsx`, `.mjs`, `.cjs`), and CSS.
  - Swift (`//`, `///` and nested `/* */`). It skips multi-line `"""` strings.

  Only tool directives pass. In TypeScript, that is a triple-slash `/// <reference ...>`. In Swift, it is the `// swift-tools-version:` line. It does not parse JSX text. Thus, to put a `//` in text between tags, use a string expression. It walks `.github/` but no other dot dir. It skips `vendor/`, `node_modules/`, the root `bin/`, every `dist/` (thus the built app in `internal/serve/dist`), and Go files under `testdata/`. See [ADR 0044](../docs/adr/0044-no-comments.md). Its fixtures are in its `testdata/` as `*.<ext>.txt`, so that no other tool reads them.
- **`lint-agents`** (`make lint`): fails when one of these conditions is true:
  - The root `AGENTS.md` is longer than 150 lines (`-max n`).
  - A scoped `AGENTS.md` has no `CLAUDE.md` symlink to it beside it.
  - A relative link in an `AGENTS.md` or `ARCHITECTURE.md` does not resolve.

  It skips `testdata/`, `.claude/`, `.git/`, `node_modules/`, `vendor/`, `bin/` and `dist/`. Its fixtures are directory trees under its `testdata/`. See [ADR 0041](../docs/adr/0041-scoped-agents-md.md).
- **`tdd-check <base> <head>`**: the check that CI's `tdd` job runs.
  - **Files.** It covers added or changed `*_test.go` files and files under a `testdata/` dir (such as e2e `.txtar` scripts). Each file counts for the package that owns it. It runs them with `-tags integration`, so integration tests count too. It skips `fakes_test.go`/`*_fake_test.go`.
  - **Commit order.** First, it fails when a non-merge commit in `base..head` changes tests and production code together. A commit whose subject starts with `refactor:` or `refactor(` is exempt, and so are Markdown files (ADR 0043). A moved and edited test counts in both checks. The base run removes its old path and each test file that the PR deleted.
  - **Ignored changes.** It ignores a Go test change when the Go tokens of the file did not change. Thus whitespace and comments of both styles do not count, but whitespace in a string literal does. In other testdata, it ignores changes in the amount of whitespace, blank lines, and `//` and `#` comment lines. The exception is `*.golden` and `*.txt` fixtures, where comment-shaped lines are data (ADR 0045).
  - **Web tests** have parity with Go ([ADR 0048](../docs/adr/0048-web-toolchain-and-tdd-parity.md)). `web/**/*.test.ts(x)` and web `testdata/` count for the commit-order rule. On base, it runs `npm ci` in `web/`, then `npx vitest run` one time for each changed test file (or one time for the directory that owns changed web testdata). If one passes, the job fails. A base without `web/package.json` counts as failing. A failed `npm ci` fails the job. Helpers under `web/src/test/` are exempt, like port fakes. `TDD_WEB_INSTALL` and `TDD_WEB_TEST` replace the two commands. Its tests use them to run without Node.
  - **Swift tests** have the same parity ([ADR 0049](../docs/adr/0049-macos-app.md)). `macos/**/Tests/**/*.swift` count as tests. On base, it runs `swift test --filter <Target>.<File>` in `macos/` one time for each changed test file. `<Target>.<File>` is the path under `Tests/`, with `/` as `.` and no `.swift`. Thus the name of a test file must match the type name of its suite. A base without `macos/Package.swift` counts as failing. `TDD_SWIFT_TEST` replaces `swift test --filter`. Its tests use it to run without Swift.
  - **Its tests** are in `scripts/tddcheck`. They build temporary repos and run the script on them.
  - **Local runs.** Before you push, run it locally on a clean tree to check a branch. It refuses uncommitted changes. It resolves both refs to commits first, so `HEAD` works. On exit, pass or fail, it puts back the branch or commit that you started on, with a clean index.
- **`mutate`** (`make mutate`): runs `gremlins` on `domain` and `app`. It fails below 80% efficacy. It skips a package with no tests. Install it with `go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`.
- **`bench-hook.sh [bin]`**: times 200 `agentws hook` runs with the daemon up and down. It prints p50/p95 and fails over budget (20 ms / 60 ms p95). Each side gets 20 warmup runs that it does not count, then three rounds of 200. It judges the p95 of the lowest round. A noisy neighbour only adds time, and one bad burst must not fail a fast hook. CI runs it with `BUDGET_SCALE=2`, because its runners start processes about 3x slower.
- **`dev`** (`make dev [SEED=3] [FAKES=1]`): runs agentws from this checkout and attaches to the dev layout.
  - It uses its own `AGENTWS_HOME` (`/tmp/agentws-dev-$UID`) and tmux socket (`agentws-dev`). Thus the installed agentws and `~/.agentws` do not change.
  - In the background, it polls the Go sources each second. On a change, it builds again, restarts the dev daemon and starts the dev TUI pane again on the new binary. Sessions continue, as they do across all daemon restarts.
  - A failed build does not stop the running instance. It shows the first error on the dev tmux server. The full output goes to `dev.log` in the dev home.
  - `SEED=n` runs `agentws debug seed n`. `FAKES=1` puts the e2e fake `claude` and `codex` first on `PATH`. They hold their pane and use no quota.
  - `scripts/dev stop` stops the watcher and the dev daemon.
  - `AGENTWS_DEV_HOME` and `AGENTWS_DEV_SOCKET` move the instance. Keep the home short and under `/tmp`, because macOS limits Unix socket paths to 104 bytes. See [ADR 0039](../docs/adr/0039-dev-loop-and-build-handshake.md).
- **`screenshot <out.png> [COLSxROWS] < steps`**: renders a pane to PNG for the body of a UI PR.
  - It builds, and starts a temporary daemon and tmux server. It runs `agentws tui --new-session` (or a `run <command>` step), with the launch dir set to the cwd.
  - It plays the stdin steps: `type <text>`, `key <tmux keys>`, `wait <s>`, and `shot <file>` for an intermediate frame. It renders with `freeze` (`go install github.com/charmbracelet/freeze@latest`).
  - It splits a `run` step like a shell line (quotes kept, no globbing) and shell-quotes each word. Only a word that is exactly `agentws` becomes the built binary. It skips blank lines.
  - The pane runs with true color. It is rendered on Latte's base (`SHOT_BACKGROUND`, default `#eff1f5`), because agentws paints no background of its own.
  - All that it starts (daemon, tmux server, pane, popups) runs with a temporary `HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `CLAUDE_CONFIG_DIR` and `CODEX_HOME` in its temp `AGENTWS_HOME`. That home is marked onboarded, so the first-run walkthrough never shows. It builds before it changes `HOME`. It refuses a step that sets a variable to the real `HOME` or a path under it.
  - Its tests are in `scripts/screenshottest`, with fake `agentws`, `tmux` and `freeze`.
  - Run it from a demo folder tree, and pass `run env HOME=<demo home> agentws ...`, so that no real paths show. The images are on the orphan `pr-assets` branch, not in main.
- **`web-screenshot < steps`**: renders the web app for the body of a PWA PR. It starts the Vite dev server with the fake API on 127.0.0.1 (`WEB_SHOT_PORT`, default 5199). The fake API (`web/fake-api.ts`) does these things:
  - `hello` reports `v1` and build `v0.12.0+demo`.
  - Pairing accepts `ABCD2345`, answers `ZZZZZZZZ` with 429 and all other codes with 401.
  - `/api/v1/stream` sends a demo state with every group, both limits and two queued sends on the running `s3`.
  - `GET …/messages` answers each session with a chat of tool rows (done with a test result, failed, running) and Markdown. It answers its older page after 5 s, so a shot can catch the loading row.
  - Send, unsend and interrupt answer success.
  - With `AGENTWS_FAKE_STREAM=offline`, it drops the first stream after 300 ms and refuses the rest, for the offline state.
  - With `AGENTWS_FAKE_STREAM=late`, it closes the first stream with 4401 after 300 ms, while `GET /api/v1/workspaces` still answers. Thus the app keeps its login and connects again.
  - With `AGENTWS_FAKE_STREAM=revoked`, it does the same, but `GET /api/v1/workspaces` answers 401. This gives the pairing screen with its signed-out note.

  It controls Playwright's Chromium from stdin steps: `device <Playwright device>` (default `iPhone 15`), `mode browser|installed` (installed sets `navigator.standalone`), `goto <path>`, `fill <label>=<text>`, `click <button name>`, `see <text>`, `wait <ms>`, `shot <file>` (full page, relative to the cwd). `device` and `mode` start a fresh browser context. Playwright is pinned in `web/package.json`. Install its browser one time with `cd web && npx playwright install chromium`. Push the images to the orphan `pr-assets` branch, as for `screenshot`.
- **`install.sh`**: installs a release and copies `nvim/lua` and `nvim/plugin` to `${XDG_DATA_HOME:-~/.local/share}/agentws/nvim`. To try a release build:
  1. Run `goreleaser release --snapshot --clean`.
  2. Serve `dist/` with `python3 -m http.server`.
  3. Run `scripts/install.sh` with `AGENTWS_DOWNLOAD_BASE`, a temp `AGENTWS_INSTALL_DIR` and a temp `XDG_DATA_HOME`.

  See [ADR 0040](../docs/adr/0040-first-run-walkthrough.md).
