# Automations

Automations are `[[automation]]` tables in `$CREW_HOME/config.toml` or a repo's `.crew.toml`. They start sessions on demand (`a` in the TUI, `crew automation run`, the PWA) or on a schedule. With no automations, every surface says how to add one.

## Setup

```sh
go run ./scripts/verify iso down
go run ./scripts/verify iso up
go run ./scripts/verify iso seed --sessions 3
```

## Recipes

### CLI empty state tells how to add one

```sh
go run ./scripts/verify iso crew --expect "no automations" --expect "[[automation]]" -- automation list
```

### TUI a shows the empty state

```sh
go run ./scripts/verify tui drive --restart --out-dir /tmp/crew-verify-map/automations --steps '[{"expect":"seeded change 2","timeout":8000},{"keys":"a"},{"expect":"no automations","timeout":5000},{"shot":"empty.png"}]'
```

### PWA automations tab at both viewports

```sh
go run ./scripts/verify web drive /#/automations --viewport both --out-dir /tmp/crew-verify-map/automations --steps '[{"expect":{"role":"link","name":"Automations"}},{"shot":"tab.png"}]'
```

Expect: the empty state, no console errors, no failed API calls.

### run an automation from config (manual: needs an [[automation]] table in the isolated config.toml and a harness that runs turns; the fake claude does not)

## Cleanup

```sh
go run ./scripts/verify iso down
```

## Gotchas

- A repo `.crew.toml` automation must be trusted (`crew automation trust <name>`) before it runs.
