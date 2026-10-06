# internal/serve

`agentws serve`: the HTTP and WebSocket API the phone app (PWA) talks to. It is a separate process that reaches the daemon only through `rpc`, like the TUI, and exposes an allowlist of it, never a passthrough. See [ADR 0046](../../docs/adr/0046-remote-app.md); pairing and revocation on the daemon side are in [internal/daemon/](../daemon/AGENTS.md).

Tests: `go test ./internal/serve/ ./cmd/agentws/ -run Serve` (an in-memory daemon fake in `fakes_test.go`, `httptest` and a real WebSocket client) and `go test -tags integration ./test/integration/ -run ServeAgainst` (a real daemon with SQLite). Golden responses are in `testdata/`; regenerate with `go test ./internal/serve -update` and review the diff, since a changed golden is a changed API.

## Layers

- depguard: `internal/serve` imports only `rpc` and `domain` from `internal/`, plus the standard library and `github.com/coder/websocket`. No `os/exec`: `cmd/agentws/serve.go` parses the flags, reads `[serve] url`, and passes a `Dial` that starts the daemon the way the TUI does (`rpc.Connect` with `spawn`).
- `serve.Daemon` is the slice of `*rpc.Client` serve uses (`Call`, `Subscribe`, `WatchTranscript`, `Close`). Every HTTP request dials its own connection and closes it; every stream holds its own connection, so a slow phone never stalls another client's reader.

## Listening

- `serve.Options{Addr, Cert, Key, SelfSigned, CertDir}`; `Check` is the rule: plain HTTP only on a loopback address (`localhost`, `127.0.0.0/8`, `::1`), whatever the other flags. `--cert` and `--key` go together; `--self-signed` replaces them.
- `--self-signed` keeps `cert.pem` and `key.pem` (600) in `$AGENTWS_HOME/serve`, valid 5 years, for `localhost`, `127.0.0.1`, `::1`, `host.docker.internal`, the hostname and the `--addr` host. It is reused while it has 30 days left and covers the address, so a proxy that trusts it keeps working across restarts. When serve replaces it (a new `--addr` host, or near expiry), the proxy must be set to trust the new one.
- `Server.Start(ctx)` opens the revocation `subscribe` connection; call it before serving. It fails when the daemon cannot be reached, and resubscribes every second after the daemon restarts. Cancelling `ctx` closes every open stream with 1001.

## HTTP API (`/api/v1`)

Every response is JSON with `Cache-Control: no-store`. An error is `{"error":{"code","message"}}` with the daemon's `rpc` code and an HTTP status: `bad_request` 400, `unauthorized` 401, `forbidden` 403, `not_found` 404, `stale` 409, `rate_limited` 429, `failed`/`launch_failed` 500, `unknown_method` 501, `version_mismatch` 502, `unavailable` 503 (also when the daemon cannot be reached). Unknown paths under `/api/` answer `not_found`.

| Endpoint | Auth | Daemon method | Body → response |
|---|---|---|---|
| `GET /api/v1/hello` | no | none | → `{"api":"v1","build"}` (serve's build, which is the daemon's) |
| `POST /api/v1/pair` | no | `pair.redeem` | `{"code","name"}` → `{"device":{"id","name","created_at","last_seen"},"token"}` |
| `GET /api/v1/workspaces` | yes | `workspace.list` | → `{"workspaces":[Workspace],"last_used"}` |
| `POST /api/v1/sessions` | yes | `session.new` | `{"workspace","work_item","harness","model","effort","prompt"}` (`work_item` and `harness` required; the rest optional) → the new `Session`; a setup recipe failure is `failed` with its output in `message` |
| `GET /api/v1/work-items/resolve?workspace=&item=` | yes | `session.resolve` | → `{"source","ref","title","worktree","workspace"}`; `bad_request` for an empty item or an unsupported link, `not_found` for an unknown workspace or an item the tracker does not know |
| `GET /api/v1/sessions/{id}/messages?before=&limit=` | yes | `transcript.page` | → `{"messages":[Message],"before"}`; both query values optional integers |
| `POST /api/v1/sessions/{id}/end` | yes | `session.end` | → the ended `Session` |
| `POST /api/v1/sessions/{id}/resume` | yes | `session.resume` | → the resumed `Session` |
| `POST /api/v1/sessions/{id}/mute` | yes | `session.mute` | `{"muted":bool}` (required) → `{}` |
| `POST /api/v1/sessions/{id}/rename` | yes | `session.rename` | `{"name"}` → `{}` |
| `POST /api/v1/sessions/{id}/messages` | yes | `session.send` | `{"text"}` (not blank) → `{"id","queued"}`; a queued send shows in the stream's `sends` until it is pasted |
| `DELETE /api/v1/sessions/{id}/sends/{send}` | yes | `session.unsend` | → `{}`; `not_found` once the send has gone out (it left `sends`) |
| `POST /api/v1/sessions/{id}/interrupt` | yes | `session.interrupt` | → `{}` |
| `GET /api/v1/sessions/{id}/prompt` | yes | `session.prompt` | → `{"id","text","choices":[{"id","label"}],"raw"}` (`id` identifies the dialog shown: a hash of its text and choices); `raw` (the last 40 visible pane lines, with empty `text` and `choices`) comes when the session is in `permission` but its dialog is not recognized; `not_found` when no dialog is showing |
| `POST /api/v1/sessions/{id}/answer` | yes | `session.answer` | `{"choice","prompt"}` (a choice `id` from the prompt, not blank; `prompt` is the prompt `id` that was shown) → `{}`; `stale` (409) when the session is no longer in `permission` or the dialog showing is not the `prompt` given, in which case nothing was sent |
| `GET /api/v1/push/key` | yes | `push.key` | → `{"public_key"}`, the VAPID public key (base64url), for `pushManager.subscribe` |
| `POST /api/v1/push/subscribe` | yes | `push.subscribe` | the browser's `PushSubscription.toJSON()` (`{"endpoint","keys":{"p256dh","auth"}}`) → `{}`; stored with the calling device |
| `POST /api/v1/push/unsubscribe` | yes | `push.unsubscribe` | → `{}`; drops the calling device's subscription (the app's sign out) |

- Auth is `Authorization: Bearer <token>`, checked with `device.check` on the request's own connection before the method runs, and then against the set of revoked device IDs.
- `pair` passes the caller's address as `addr`: `RemoteAddr`, or the last `X-Forwarded-For` entry (else `X-Real-IP`) when the peer is a loopback or private address, which is where a proxy in front of serve sits. A forged header from a direct client is ignored.
- Domain structs (`Workspace`, `Session`, ...) encode with their Go field names, as in the socket protocol; the goldens pin them. A `Message` is `rpc.Message`: `{"id","cursor","turn","role","text","tool":{"name","summary","status"},"at"}`; keep messages by `id` and replace by `id` (see [internal/rpc/](../rpc/AGENTS.md)).
- Bodies are capped at 64 KiB.
- A route reads the device that made the request with `deviceOf(r)` (set by the auth check); `push/subscribe` passes its ID to the daemon.
- Push is sent by the daemon, not by serve: serve only forwards the key and the subscription. See "Web Push" in [internal/daemon/](../daemon/AGENTS.md).

### Adding an endpoint

1. Write the failing test in `server_test.go`: the request, the daemon method and params the fake saw, and a golden for the response.
2. Add one line to `Server.routes()`: a Go 1.22 pattern (`"POST /api/v1/sessions/{id}/send"`) and a `func(r *http.Request, d Daemon) (any, error)`. Routes there are authenticated; return a value to encode or an error (an `*rpc.Error` keeps its code). `sessionAction(method, params)` covers "decode a body, add the session id, call one method, pass the result through".
3. Add the method to `allowed` in `server_test.go` and the endpoint to `authedEndpoints`, so the token and allowlist tests cover it, and add a row to the table above.

The `/` fallback: `Handler(fallback)` mounts `fallback` at `/`; `Handler(nil)` serves the API only. `cmd/agentws/serve.go` passes `Static(Dist())`, the embedded PWA.

## The embedded web app (`static.go`)

- `Dist()` is `dist/` embedded with `//go:embed all:dist`. `make web` builds [web/](../../web/AGENTS.md) into it and writes the shell as `app.html`; the committed `index.html` is the placeholder a Node-less `go build` embeds ("run make web"). See [ADR 0048](../../docs/adr/0048-web-toolchain-and-tdd-parity.md).
- `Static(fsys)` serves `app.html` (else the placeholder) for `/` and for any extensionless path that is not a file (client routes), `no-cache`; `assets/*` is hashed and `immutable`; other files (`sw.js`, the manifest, icons) are `no-cache`; `/api` and below are 404, though behind `Handler` the API's own JSON `not_found` answers first. Only `GET` and `HEAD`.
- Tests: `go test ./internal/serve/ -run Static`; after `make web`, `AGENTWS_WEB_BUILT=1` also checks the embedded build.

## Stream (`GET /api/v1/stream`, WebSocket)

- The `Origin` header must equal the public URL's origin (`--url`, else `[serve] url`; a URL without a scheme means `https`; default ports are ignored). Another or a missing `Origin`, or no public URL, is refused with 403 before the upgrade.
- The first client frame must be `{"token":"<device token>"}` within 5 s. A wrong, missing or late token closes the socket with code 4401.
- Then the server sends `{"state":{"seq","workspaces","tasks","worktrees","sessions","limits","queue","sends"}}` and one `{"diff":{...}}` per change that touches those: a diff sets `seq` and one of `workspace`, `task`, `worktree`, `session`, `limits` (whole list), `queue` (whole list), `sends` (whole list), `removed_workspace`, `removed_worktree`, `removed_session`. Events, subagents, review drafts and comments are dropped, so `seq` has gaps. Worktrees come without `Ports`.
- Each stream keeps a `view` (`view.go`) of tasks, worktrees, sessions and their last `domain.SessionEventsKept` events, so a session is sent as a `StreamSession`: the Go fields plus `name` (`domain.NameFor` with its worktrees' PRs), `where` (`domain.WorktreeLabel`, `repo@branch +N`), `banner` (the body of `domain.BannerFor` for its state, also for a muted session; empty while running or idle) and `since` (`domain.StateSince`, when the replayed events last changed its state; `null` when they do not explain it). The phone renders these and never re-derives them. A task or worktree diff that changes another session's derived fields is followed by a `session` diff with the same `seq`.
- `limits` is `domain.Quotas` of the sessions as `StreamQuota`: the Go fields plus `label` (`domain.WindowLabel`), `low` and `stale_at` (`ReportedAt` + `domain.StaleQuotaAfter`). It comes in a diff only when a session change or removal changes it. The client hides windows whose `ResetsAt` (Unix seconds, 0 unknown) has passed and dims those past `stale_at`, like the TUI's limits bar.
- `{"watch":"<session>","after":<cursor>}` adds a session's messages (`after` is the largest `cursor` the client holds, 0 for all) through `transcript.watch` on the stream's connection. Frames: `{"transcript":{"session","messages":[...]}}` first with the messages after the cursor, then one per change; `"reset":true` means the session moved to another transcript file (drop its cursor; the messages that follow start the new file), and `"closed":true` means the session is gone and the watch ended. Watching a session again replaces its watch; at most 32 per stream.
- `{"unwatch":"<session>"}` ends it; closing the stream ends all of them.
- `{"visible":bool}` says whether the page is on screen. serve passes it to `device.viewing` on the stream's own daemon connection, so it ends with the stream; the daemon skips pushes to a device while it is visible (see "Web Push" in [internal/daemon/](../daemon/AGENTS.md)). There is no reply; a failed call is ignored, which only means the device still gets pushes. A failed watch gets `{"error":{"code","message"},"watch":"<session>"}`.
- An unknown client frame gets `{"error":{"code":"bad_request","message"}}` and the stream stays open.
- Close codes: 4401 unauthorized or revoked, 1013 the daemon went away (reconnect), 1001 serve is stopping (also while a stream waits for its token), 1011 a write failed.
- Revocation: a stream subscribes before it runs `device.check` on the same connection, so the daemon's loop orders them: a revoke before the check fails it, a revoke after reaches the stream's own subscription as a `revoked_device` diff. Either that diff or the one on the `Start` connection cancels every open stream of that device (4401 at once) and records the ID, so REST checks that answered just before it still fail. The stream does not depend on the `Start` connection, which may be reconnecting after a daemon restart. The stream reads its subscription in its own goroutine and queues the diffs for the writer, so a phone that stops reading (a write blocked up to 10 s) still sees its revocation at once; the canceled write closes the socket, with 4401 when the transport is still open.
