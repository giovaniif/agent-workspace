# Worktrees and cleanup

crew tracks each worktree with its repo, branch, PR and session. Cleanup removes a worktree after its PR merges. It backs up uncommitted changes first, never deletes branches, and never touches a worktree that a process uses. `crew cleanup` with no flag only reports what it would do.

## Setup

```sh
go run ./scripts/verify iso down
go run ./scripts/verify iso up
go run ./scripts/verify iso seed --sessions 3
```

## Recipes

### worktree list shows the seeded worktrees and PRs

```sh
go run ./scripts/verify iso crew --expect seed-1-api --expect "#100 OPEN failing" --expect seed-3-infra -- worktree list
```

### cleanup dry run keeps worktrees it cannot read

```sh
go run ./scripts/verify iso crew --expect "keep" --expect "kept" --absent "removed" -- cleanup
```

Expect: each seeded worktree is `keep` with `facts unavailable`, because the folders are not repos.

### cleanup of a merged worktree on a temp repo (manual: needs a temp repo with a merged PR in the fake gh; the rules are in internal/domain cleanup tests and internal/app tests)

## Cleanup

```sh
go run ./scripts/verify iso down
```

## Gotchas

- Do not run `d` or `b` in the disk view on a real repo. They really remove worktrees.
