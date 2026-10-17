# Feature map

This folder is the shared memory of what "working" means in crew (ADR [0055](../docs/adr/0055-agent-verification.md)). Each file is one area. Each recipe is a set of `go run ./scripts/verify ...` commands that `verify map run` runs.

## Areas

| Area | File | Surfaces |
|---|---|---|
| Sessions | [sessions.md](sessions.md) | TUI, PWA |
| Review pane | [review-pane.md](review-pane.md) | TUI |
| Worktrees and cleanup | [worktrees-cleanup.md](worktrees-cleanup.md) | CLI, TUI |
| PR board | [pr-board.md](pr-board.md) | CLI |
| Disk view | [disk-view.md](disk-view.md) | TUI |
| Automations | [automations.md](automations.md) | CLI, TUI, PWA |
| Mates | [mates.md](mates.md) | CLI |
| Approvals | [approvals.md](approvals.md) | CLI, PWA |
| Remote, serve and pairing | [remote-pairing.md](remote-pairing.md) | CLI, HTTP |
| PWA | [pwa.md](pwa.md) | PWA |
| Notifications | [notifications.md](notifications.md) | CLI |
| Setup and onboarding | [setup-onboarding.md](setup-onboarding.md) | TUI |
| nvim | [nvim.md](nvim.md) | TUI |
| Mac app | [mac-app.md](mac-app.md) | CI only |

## Format

- `## Setup` and `## Cleanup`: shell blocks that `map run` runs before and after the recipes. Cleanup runs also after a failure.
- `## Recipes`: one `### <label>` for each recipe, then one shell block. Each line must exit 0.
  - `Needs: VAR` marks a recipe that needs an environment variable. Without it, the result is `needs-var`.
  - `### <label> (manual: why)` marks a flow that the CLI cannot drive.
- `## Gotchas`: known traps and product gaps, with the issue links.

## Rules

- Use only synthetic data. Give the data that a recipe makes the `TEST-` prefix. `crew debug seed` names its data `seed-*`.
- Do not make a recipe weaker to make it pass. If the app changed, change the recipe. If the app is wrong, keep the recipe failing, add a Gotcha and open an issue.
- When you fix a bug, add its repro as a recipe.

## Commands

```sh
go run ./scripts/verify map status
go run ./scripts/verify map run sessions
go run ./scripts/verify map run sessions --recipe "sidebar lists the seeded sessions"
```
