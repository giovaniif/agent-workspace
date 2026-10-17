# Sessions

The sidebar and the PWA list every session, grouped by task, with its state, harness, model and limits. A session that needs the user (permission or waiting) is first in the PWA and counted as "need you" in the TUI. The TUI and the PWA must agree on the sessions and their states.

## Setup

```sh
go run ./scripts/verify iso down
go run ./scripts/verify iso up
go run ./scripts/verify iso seed --sessions 3
```

## Recipes

### sidebar lists the seeded sessions

```sh
go run ./scripts/verify tui drive --restart --out-dir /tmp/crew-verify-map/sessions --steps '[{"expect":"seeded change 1","timeout":8000},{"expect":"seeded change 3"},{"expect":"1 need you"},{"shot":"sidebar.png"}]'
```

Expect: two tasks (`seeded task 1`, `seeded task 2`), three sessions, the Claude 5h limit at 88%.

### j and k move the selection

```sh
go run ./scripts/verify tui drive --out-dir /tmp/crew-verify-map/sessions --steps '[{"expect":"seeded change 2","timeout":8000},{"keys":"j"},{"keys":"j"},{"expect":"opus-5.5"},{"shot":"moved.png"}]'
```

### help toggles with ?

```sh
go run ./scripts/verify tui drive --out-dir /tmp/crew-verify-map/sessions --steps '[{"keys":"?"},{"expect":"keys","timeout":3000},{"keys":"?"},{"shot":"help.png"}]'
```

### PWA groups sessions by attention at both viewports

```sh
go run ./scripts/verify web drive / --viewport both --out-dir /tmp/crew-verify-map/sessions --steps '[{"expect":{"text":"seeded change 2"}},{"expect":{"text":"Needs you","exact":false}},{"expect":{"text":"Working","exact":false}},{"expect":{"text":"Done","exact":false}},{"shot":"list.png"}]'
```

Expect: mobile and desktop show Needs you 1, Working 1, Done 1. No console errors and no failed API calls.

### PWA opens a session chat

```sh
go run ./scripts/verify web drive / --viewport mobile --out-dir /tmp/crew-verify-map/sessions --steps '[{"click":{"text":"seeded change 1"}},{"expect":{"role":"textbox","name":"Message","exact":false}},{"shot":"chat.png"}]'
```

### CLI reads back the seeded worktrees

```sh
go run ./scripts/verify iso crew --expect seed-1-api --expect "#100 OPEN failing" -- worktree list
```

### new session dialog with a real harness (manual: the fake claude holds the pane but does not run turns; use make dev FAKES=1 to try it)

## Cleanup

```sh
go run ./scripts/verify iso down
```

## Gotchas

- `crew tui --new-session` opens the new session dialog over the sidebar. `verify tui` runs `crew tui` with no flag.
- The seeded worktrees are in `/tmp/seed`. They are not git repos, so `git status` fails on them.
