# 0039. Dev loop and build handshake

## Context

Trying a change meant `make build`, `crew daemon stop`, then reattaching, and it was easy to leave an old daemon serving a new TUI. The protocol version (`v`) only changes when the wire format does, so it could not catch that.

## Decision

- `internal/version` holds `Version` and `Commit`, stamped with `-ldflags -X`. `String()` is `Version+Commit`. Release tooling stamps the same two vars.
- Every request carries `build` (the client's `version.String()`) and `built_at` (its executable's mtime). The daemon answers a request from another build with `version_mismatch`, naming the side whose binary is older as the one to restart. Every daemon response carries `build`; a client refuses an answer without one, since such a daemon predates the handshake.
- `status`, `hook` and `statusline` are answered across builds: `daemon stop` must reach a stale daemon, and hooks may come from another install's binary pointed at the same `CREW_HOME`. Raw hook lines send no build at all.
- `scripts/dev` (`make dev`) is a shell loop, not a Go command: it polls the sources each second (no watcher dependency), builds to a temp file and renames it into place, then stops and starts the dev daemon and `respawn-pane -k`s the panes running `<dev bin> tui`. Each dev build stamps a unique commit suffix so the handshake tells rebuilds apart. A failed build is shown with `tmux display-message` and the old binary keeps running.

## Consequences

A stale daemon or TUI fails loudly instead of misbehaving. A client and daemon from the same release but different binaries (for example two `go build`s without stamping) both report `dev` and still talk, which is fine for tests.
