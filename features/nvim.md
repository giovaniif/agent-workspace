# nvim

`e` toggles nvim on the worktree of the selected session. In the review, `o` opens the top visible line of the diff in nvim. The Lua plugin is in `nvim/`.

## Setup

```sh
go run ./scripts/verify iso down
go run ./scripts/verify iso up
go run ./scripts/verify iso seed --sessions 3
```

## Recipes

### the help lists the nvim key

```sh
go run ./scripts/verify tui drive --restart --out-dir /tmp/crew-verify-map/nvim --steps '[{"expect":"seeded change 2","timeout":8000},{"keys":"?"},{"expect":"nvim (o in a review opens the line)","timeout":3000}]'
```

### e opens nvim in the slot (manual: the seeded worktrees do not exist on disk, and the TUI runs alone, not in the crew client layout)

## Cleanup

```sh
go run ./scripts/verify iso down
```

## Gotchas

- `verify tui` runs `crew tui` in a plain tmux session, not in the client layout. Keys that swap panes into the slot (`enter`, `t`, `e`) need `make dev`.
