# Setup and onboarding

`S` opens "Set up crew": pick the agents, set up each one, then Neovim. The setup commands merge into the agent config idempotently, with a backup. In the isolated crew, `HOME`, `CLAUDE_CONFIG_DIR` and `CODEX_HOME` are temporary, so the real config never changes. `iso up` marks the home onboarded, so the first-run walkthrough does not show.

## Setup

```sh
go run ./scripts/verify iso down
go run ./scripts/verify iso up
```

## Recipes

### S opens the setup with the three agents

```sh
go run ./scripts/verify tui drive --restart --out-dir /tmp/crew-verify-map/setup --steps '[{"expect":"crew","timeout":8000},{"keys":"S"},{"expect":"Set up crew","timeout":5000},{"expect":"Claude Code"},{"expect":"Codex"},{"expect":"Oh My Pi"},{"shot":"setup.png"},{"keys":"Escape"}]'
```

### first-run walkthrough (manual: iso up marks the home onboarded; remove $CREW_HOME/onboarded in the isolated home and restart the TUI to see it)

## Cleanup

```sh
go run ./scripts/verify iso down
```

## Gotchas

- Never run the setup outside the isolated crew. It edits `~/.claude` and `~/.codex`.
