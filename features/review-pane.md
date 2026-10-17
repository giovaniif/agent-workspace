# Review pane

`r` opens the local, PR-style review of the selected session. The sidebar widens, the list becomes a rail, and the review shows the scope tabs (`last turn`, `uncommitted`, `branch vs base`), the worktree tabs, the file tree and the diff. `r` or `esc` closes it.

## Setup

```sh
go run ./scripts/verify iso down
go run ./scripts/verify iso up
go run ./scripts/verify iso seed --sessions 3
```

## Recipes

### r opens the review with scopes and worktrees

```sh
go run ./scripts/verify tui drive --restart --out-dir /tmp/crew-verify-map/review --steps '[{"expect":"seeded change 2","timeout":8000},{"keys":"r"},{"expect":"REVIEW","timeout":5000},{"expect":"last turn"},{"expect":"branch vs base"},{"expect":"api:part-1"},{"shot":"review.png"}]'
```

Expect: the rail on the left, the transcript of the session, and `0 files` for the seeded worktrees.

### esc closes the review

```sh
go run ./scripts/verify tui drive --restart --out-dir /tmp/crew-verify-map/review --steps '[{"expect":"seeded change 2","timeout":8000},{"keys":"r"},{"expect":"REVIEW","timeout":5000},{"keys":"Escape"},{"absent":"REVIEW","timeout":5000},{"expect":"SESSIONS"}]'
```

### ] changes the scope

```sh
go run ./scripts/verify tui drive --restart --out-dir /tmp/crew-verify-map/review --steps '[{"expect":"seeded change 2","timeout":8000},{"keys":"r"},{"expect":"REVIEW","timeout":5000},{"keys":"]"},{"wait":800},{"shot":"scope.png"}]'
```

### diff, comments and send on a real repo (manual: the seeded worktrees are not repos; the review needs a session on a temp repo with changes, see internal/adapters/git tests)

## Cleanup

```sh
go run ./scripts/verify iso down
```

## Gotchas

- The seeded worktrees under `/tmp/seed` are not git repos. Thus every scope shows `no changes`.
