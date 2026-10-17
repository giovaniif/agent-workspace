# PR board

`crew pr <session>` shows the PRs of a session: the blockers (failing checks, requested changes, open threads), the failing checks with links, and the bot comments since the last push. `--json` gives the same data for agents.

## Setup

```sh
go run ./scripts/verify iso down
go run ./scripts/verify iso up
go run ./scripts/verify iso seed --sessions 3
```

## Recipes

### PR board of the first seeded session

```sh
go run ./scripts/verify iso crew --expect "#100" --expect "checks failing" --expect "unit tests" --expect "bot comments since push: 3" -- pr seed-session-1
```

### PR board as JSON

```sh
go run ./scripts/verify iso crew --expect '"number"' --expect '100' -- pr seed-session-1 --json
```

### unknown session fails

```sh
sh -c '! go run ./scripts/verify iso crew -- pr TEST-no-such-session >/dev/null'
```

## Cleanup

```sh
go run ./scripts/verify iso down
```

## Gotchas

- The PR data comes from the seed, not from GitHub. The fake `gh` on `PATH` answers `gh api` with `$CREW_E2E/prs.json`.
