---
name: verify
description: Start an isolated crew, drive the TUI and the PWA, read Mac app CI evidence, and prove a change with an evidence bundle. Use it before you claim that a crew change works, to reproduce a bug, to run the feature map, or to make PR screenshots.
---

# Verify a crew change

Use `go run ./scripts/verify` for all work on a running crew. Each command prints one JSON object. Read `ok`, then each `problems[].msg` and `problems[].hint`. Exit 0 is ok, 1 is a failure, 2 is a usage error. The full command list is in `scripts/AGENTS.md` (the `verify` entry). The design is ADR 0055.

## Rules

- Never touch the real `~/.crew`, `~/.claude`, `~/.codex`, `~/.config/nvim` or the `crew` tmux server. Only use the isolated crew.
- Use only synthetic data. Give the data that you make the `TEST-` prefix (`test-` where a name must be lower case).
- Never log in with a real account. The PWA uses the device that `iso up` pairs.
- Tests alone are not proof. Drive the real surface.
- You are not the verifier of your own PR. A fresh agent posts the verdict (see `scripts/land`).

## 1. Start

```sh
go run ./scripts/verify doctor
go run ./scripts/verify iso up
go run ./scripts/verify iso seed --sessions 3
```

- `doctor` names each missing tool with the install command.
- `iso up` builds crew from the checkout. After each checkout or code change, run `iso down` and `iso up` again. The isolated crew does not watch the sources.

## 2. Drive

TUI (tmux server `<iso socket>-tui`):

```sh
go run ./scripts/verify tui drive --restart --steps '[{"expect":"seeded change 2","timeout":8000},{"keys":"w"},{"expect":"WORKTREES & DISK"},{"shot":"disk.png"}]'
```

- Steps: `keys` (tmux key names, for example `Escape`, `Enter`, `j`), `type` (literal text), `wait` (ms), `expect` and `absent` (with `timeout`), `shot`.
- On a failure, read `failure_capture`: it is the pane text at the failure.
- `verify tui` runs `crew tui` alone, not in the client layout. Keys that swap panes into the slot (`enter`, `t`, `e`) need `make dev`.

PWA (real isolated `crew serve` through Vite):

```sh
go run ./scripts/verify web shot / --viewport both
go run ./scripts/verify web drive /#/approvals --viewport both --steps '[{"expect":{"role":"link","name":"Approvals"}},{"shot":"approvals.png"}]'
```

- Locators: `{"role":"button","name":"Send"}`, `{"testId":"x"}`, `{"label":"Code"}`, `{"placeholder":"..."}`, `{"text":"..."}`. Add `"exact":false` for a substring match.
- Console errors, page errors and failed `/api/` calls fail the command. On a failure, read the `failure` shot and the `*-failure.aria.txt` snapshot.
- `--fake` uses `web/fake-api.ts` with no `iso`. `--installed` sets standalone mode. `--scheme dark`. `--video`.

CLI of the isolated crew:

```sh
go run ./scripts/verify iso crew --expect "#100" -- pr seed-session-1
```

Mac app: the server has no macOS. Use CI results and snapshots only:

```sh
go run ./scripts/verify mac evidence --pr <n>
```

## 3. Feature map

```sh
go run ./scripts/verify map status
go run ./scripts/verify map run <area>
```

- The areas are in `features/`. Read `features/README.md` first.
- A failing recipe is map drift (fix the recipe) or a product bug (keep it failing, add a Gotcha, open an issue). Never make a recipe weaker to pass.
- When you fix a bug, add its repro as a recipe.

## 4. Prove a fix (the proof bar)

1. Commit the regression test alone. Then commit the fix.
2. Make the bundle:

   ```sh
   go run ./scripts/verify evidence init /tmp/ev-<n> --claim "<what the user sees and why it is wrong>" --repo . --head <fix sha>
   ```

3. Check out the base (`base` in the result) with the test commit applied, `iso down`, `iso up`. Then record:

   ```sh
   go run ./scripts/verify evidence add /tmp/ev-<n> --ref base --kind repro --artifact <shot> [--api-failure "GET /api/v1/x 500"]
   go run ./scripts/verify evidence add /tmp/ev-<n> --ref base --kind test --run "go test ./internal/x -run TestY"
   ```

   The base test must exit non-zero. If no repro is possible, add `--kind note --note "no-repro: <why>"`.
4. Check out the head, `iso down`, `iso up`. Then record:

   ```sh
   go run ./scripts/verify evidence add /tmp/ev-<n> --ref head --kind fix --viewport mobile --artifact <shot>
   go run ./scripts/verify evidence add /tmp/ev-<n> --ref head --kind fix --viewport desktop --artifact <shot>
   go run ./scripts/verify evidence add /tmp/ev-<n> --ref head --kind test --run "make test"
   go run ./scripts/verify evidence add /tmp/ev-<n> --ref head --kind design --run "go run ./scripts/verify design-check"
   ```

   For a TUI fix, use `--viewport tui`. A PWA fix needs both `mobile` and `desktop`.
5. Run `go run ./scripts/verify evidence verify /tmp/ev-<n>`. It must be `ok`.

`evidence add` refuses a proof when the checkout is not at its ref. Do not edit artifacts after you record them.

## 5. PR images

```sh
go run ./scripts/verify annotate --before base.png --after head.png --crop 0,0,780,600 --box 20,200,760,320 --caption-before "Before" --caption-after "After" --out pair.png
go run ./scripts/verify assets push --dir <pr or issue number> pair.png head.png
go run ./scripts/verify assets check
```

Embed the `urls` from `assets push`. They are pinned to a commit of the orphan `pr-assets` branch. Never commit images on the PR branch.

## 6. Clean up

```sh
go run ./scripts/verify iso down
```

`iso down` stops serve, Vite, the daemon and both tmux servers, and removes the isolated home. Run it also after a failure.
