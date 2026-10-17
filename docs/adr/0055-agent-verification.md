# ADR 0055: Agent verification

Status: accepted, 2026-10-17.

## Context

Agents write most PRs in this repo. Unit tests, e2e scripts and CI show that the code compiles and that the tested rules hold. They do not show that the TUI, the PWA and the Mac app work for a user. A human reviews each PR by hand. This is slow, and the human cannot check each surface each time.

We want agents to find, fix, prove and land small PRs. The verifier must not be the author. A PR claim must have proof that a different agent can check again.

## Decision

We add a set of tools and docs. Each tool is a Go program under `scripts/`. It runs with `go run`, like `lint-comments` and `tdd-check`. It is not a part of the `crew` binary.

### The pieces

| Piece | Where | What it does |
|---|---|---|
| Verification CLI | `scripts/verify` | The only way that agents drive a running crew. Each command prints one JSON object. |
| Feature map | `features/` | One Markdown file for each crew area, with recipes that the CLI runs. |
| Evidence bundle | a folder that `verify evidence init` makes | The proofs of one PR claim, pinned to the base and head commits. |
| Land gate | `scripts/land` | It decides if a PR is ready, merges it, and writes the daily review. |
| Verification skill | `.claude/skills/verify/SKILL.md` | How to start the isolated crew, drive it, prove a change and clean up. |
| Babysit skill | `.claude/skills/babysit/SKILL.md` | How to get a PR to ready: CI, CodeRabbit, threads and the branch. |

### Output rules of the CLIs

- Each command prints exactly one JSON object on stdout, also on an error.
- Each object has `ok` (bool) and `problems` (a list of `{msg, hint}`). It also has the data of the command.
- The exit code is 0 when `ok` is true, 1 when the command failed, and 2 for a usage error.

### The isolated crew

- `verify iso up` starts a crew that is fully separate from the real one. It has:
  - a `CREW_HOME` under `/tmp`
  - its own `CREW_TMUX_SOCKET`
  - its own TCP ports
  - a temporary `HOME`, `XDG_CONFIG_HOME`, `CLAUDE_CONFIG_DIR` and `CODEX_HOME`
  - the e2e fake `claude` and `codex` first on `PATH`.
- `verify iso seed` adds synthetic data with `crew debug seed`. All test data has the `TEST-` prefix.
- The isolated crew sends nothing out. Slack, ntfy, `terminal-notifier` and `gh` are blank or fakes on `PATH`.
- The PWA connects to a real isolated `crew serve` with a paired device token. It never uses a real account.

### Surfaces

| Surface | How the CLI drives it |
|---|---|
| TUI | `verify tui shot` and `verify tui drive` send keys to the isolated tmux server and read the pane. They render with the `scripts/screenshot` tools (`freeze`). |
| PWA | `verify web shot` and `verify web drive` control the Playwright Chromium that `web/` pins. The viewports are 390x844 and 1440x900, at 2x. |
| Mac app | `verify mac evidence` reads the results of the macOS CI jobs and their snapshot artifacts. The Linux server has no macOS, so no agent drives the Mac app live. |

### The proof bar

`verify evidence verify` passes only when all of these conditions are true:

- There is a base repro, or a `no-repro: <why>` note.
- There is a head fix. For a PWA change, there is a head fix at both viewports.
- No API call fails on head that does not also fail on base.
- A head test run passed.
- The regression test fails on base, or there is a `no-failing-test: <why>` note.
- For a UI change, there is a `design-check` result.
- No regression shows only on head.
- No artifact hash changed after the CLI recorded it.
- The checkout is still at the recorded head.

The base is the merge-base of the head with `origin/main`. One checkout holds both refs. The agent builds `crew` again after each checkout, because the isolated daemon does not watch the sources.

### The land gate

- `land check` follows step 9 of the root `AGENTS.md`. It is true only when the PR is ready: not a draft, the required checks are green, no review thread is open, the branch is up to date with `main`, and each file has a patch (fail closed). The owner does not approve each PR.
- The required checks come from the branch protection of `main`: `build`, `tdd`, `mutate` and `CodeRabbit`.
- Other findings are flags. They do not block, and the ledger and the daily review show them:
  - irreversible effects: SQL migrations that drop or rename, the release workflow, `.goreleaser.yaml`, `scripts/install.sh`, setup code that edits `~/.claude`, `~/.codex` or the nvim config, and cleanup code that removes worktrees
  - changes to CI or to the gate: `.github/`, `.coderabbit*`, `scripts/land`, `scripts/verify` and `.claude/skills`
  - no verdict for the head
- `land check --strict` makes the flags block.
- The mode is in `scripts/land/config.json`. The default is `live`: `land merge` runs `gh pr merge --merge --match-head-commit <sha>`, because this repo merges with merge commits. In `shadow` mode, `land merge` only writes the ledger.
- `land digest --since 24h` prints each PR that merged in the period, with its flags, its watch result and any revert, as JSON and as a short Markdown summary. A daily agent posts it for the owner. This daily review replaces a review of each PR before the merge.

## Why

- One JSON shape lets an agent read `ok` and `hint` and not parse text.
- An isolated crew lets agents drive a real daemon, TUI and `crew serve` with no risk to the real `~/.crew`, `~/.claude` or the `crew` tmux server.
- Proofs pinned to commits and hashes let a second agent check a claim and not trust prose.
- Go programs under `scripts/` follow the repo pattern, and Go tests cover them.

## Rejected

- **Commands in the `crew` binary:** users do not need them, and they would make the binary larger.
- **A shell or Node CLI:** the repo tests its tools with Go. The PWA parts call the Playwright that `web/` pins, through a small Node script.
- **Live control of the Mac app:** the server has no macOS. CI artifacts are the proof.

## Consequences

- PRs that change `scripts/verify`, `scripts/land` or the skills merge like other PRs. The daily review flags them.
- The feature map must stay current. A maintenance loop runs its recipes and fixes drift.
- Agents need `freeze`, `tmux`, Node 22.18+ and the Playwright Chromium. `verify doctor` reports each missing tool.
