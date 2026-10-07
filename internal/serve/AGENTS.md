# internal/serve

`agentws serve` is the HTTP and WebSocket API that the phone app (PWA) talks to. It is a separate process. Like the TUI, it gets to the daemon only through `rpc`. It gives an allowlist of the daemon methods, never a passthrough. See [ADR 0046](../../docs/adr/0046-remote-app.md). Pairing and revocation on the daemon side are in [internal/daemon/](../daemon/AGENTS.md).

Tests:

- `go test ./internal/serve/ ./cmd/agentws/ -run Serve`: an in-memory daemon fake in `fakes_test.go`, `httptest` and a real WebSocket client.
- `go test -tags integration ./test/integration/ -run ServeAgainst`: a real daemon with SQLite.

Golden responses are in `testdata/`. To make them again, run `go test ./internal/serve -update`, then review the diff. A changed golden is a changed API.

## Layers

- depguard: from `internal/`, `internal/serve` imports only `rpc` and `domain`. It also imports the standard library and `github.com/coder/websocket`. It does not import `os/exec`. `cmd/agentws/serve.go` parses the flags and reads `[serve] url`. It passes a `Dial` that starts the daemon as the TUI does (`rpc.Connect` with `spawn`).
- `serve.Daemon` is the part of `*rpc.Client` that serve uses (`Call`, `Subscribe`, `WatchTranscript`, `Close`). Each HTTP request dials its own connection and closes it. Each stream has its own connection. Thus a slow phone never stops the reader of a different client.

## Listening

- `serve.Options{Addr, Cert, Key, SelfSigned, CertDir}`. `Check` is the rule: plain HTTP only on a loopback address (`localhost`, `127.0.0.0/8`, `::1`), for all values of the other flags. Use `--cert` and `--key` together. `--self-signed` replaces them.
- `--self-signed` keeps `cert.pem` and `key.pem` (600) in `$AGENTWS_HOME/serve`.
  - The certificate is valid for 5 years. It covers `localhost`, `127.0.0.1`, `::1`, `host.docker.internal`, the hostname and the `--addr` host.
  - serve uses it again while it has 30 days or more left and covers the address. Thus a proxy that trusts it continues to work after restarts.
  - serve replaces it for a new `--addr` host, or near expiry. Then set the proxy to trust the new certificate.
- `Server.Start(ctx)` opens the revocation `subscribe` connection. Call it before you serve. It fails when the daemon is not reachable. After the daemon restarts, it subscribes again every second. A cancel of `ctx` closes all open streams with 1001.

## Log

`Config.Log` (stdout under `agentws serve`) gets one line for these events:

- A stream that closes with a code that is not 1000 or 1001: `stream closed <code> from <addr>: <reason>`.
- A 401 response: `<method> <path> 401 unauthorized from <addr>: <message>`. The path does not include its query.

The log never contains a token. It writes a maximum of `LogBurst` (20) lines for each `LogWindow` (1 min). The first line of the next window tells how many lines were dropped. `Config.Now` sets the clock for tests.

## HTTP API (`/api/v1`)

Each response is JSON with `Cache-Control: no-store`. An error is `{"error":{"code","message"}}` with the `rpc` code of the daemon and an HTTP status:

- `bad_request` 400, `unauthorized` 401, `forbidden` 403, `not_found` 404, `stale` 409, `rate_limited` 429.
- `failed`/`launch_failed` 500, `unknown_method` 501, `version_mismatch` 502.
- `unavailable` 503. This status also applies when the daemon is not reachable.

Unknown paths under `/api/` answer `not_found`.

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

- Auth is `Authorization: Bearer <token>`. Before the method runs, serve checks it with `device.check` on the connection of the request. Then it checks it against the set of revoked device IDs.
- `pair` passes the address of the caller as `addr`. This is `RemoteAddr`. If the peer is a loopback or private address (the location of a proxy in front of serve), it is the last `X-Forwarded-For` entry, else `X-Real-IP`. serve ignores a forged header from a direct client.
- Domain structs (`Workspace`, `Session`, ...) encode with their Go field names, as in the socket protocol. The goldens lock them. A `Message` is `rpc.Message`: `{"id","cursor","turn","role","text","tool":{"name","summary","status"},"at"}`. Keep messages by `id` and replace by `id` (see [internal/rpc/](../rpc/AGENTS.md)).
- The maximum body size is 64 KiB.
- A route reads the device that made the request with `deviceOf(r)`. The auth check sets it. `push/subscribe` passes its ID to the daemon.
- The daemon sends pushes, not serve. serve only forwards the key and the subscription. See "Web Push" in [internal/daemon/](../daemon/AGENTS.md).

### Adding an endpoint

1. Write the failing test in `server_test.go`: the request, the daemon method and params the fake saw, and a golden for the response.
2. Add one line to `Server.routes()`: a Go 1.22 pattern (`"POST /api/v1/sessions/{id}/send"`) and a `func(r *http.Request, d Daemon) (any, error)`. The routes there are authenticated. Return a value to encode, or an error (an `*rpc.Error` keeps its code). `sessionAction(method, params)` covers this case: "decode a body, add the session id, call one method, pass the result through".
3. Add the method to `allowed` in `server_test.go`, and the endpoint to `authedEndpoints`. Then the token and allowlist tests cover it. Add a row to the table above.

The `/` fallback: `Handler(fallback)` mounts `fallback` at `/`. `Handler(nil)` serves only the API. `cmd/agentws/serve.go` passes `Static(Dist())`, the embedded PWA.

## The embedded web app (`static.go`)

- `Dist()` is `dist/`, embedded with `//go:embed all:dist`. `make web` builds [web/](../../web/AGENTS.md) into it and writes the shell as `app.html`. The committed `index.html` is the placeholder that a `go build` without Node embeds ("run make web"). See [ADR 0048](../../docs/adr/0048-web-toolchain-and-tdd-parity.md).
- `Static(fsys)` serves only `GET` and `HEAD`:
  - `app.html` (else the placeholder) for `/` and for each path with no extension that is not a file (client routes), with `no-cache`.
  - `assets/*` is hashed and `immutable`.
  - Other files (`sw.js`, the manifest, icons) are `no-cache`.
  - `/api` and the paths below it are 404. Behind `Handler`, the JSON `not_found` of the API answers first.
- Tests: `go test ./internal/serve/ -run Static`. After `make web`, `AGENTWS_WEB_BUILT=1` also checks the embedded build.

## Stream (`GET /api/v1/stream`, WebSocket)

- The `Origin` header must be equal to the origin of the public URL. The public URL is `--url`, else `[serve] url`. A URL without a scheme means `https`. serve ignores default ports. A different or missing `Origin`, or no public URL, gets 403 before the upgrade.
- The first client frame must be `{"token":"<device token>"}`, within 5 s (`Config.AuthTimeout`).
  - If the first frame is late, is not JSON or has no token, serve closes the socket with 4400 (`CloseBadHandshake`). The client tries again. A phone that wakes up, or a slow link, must not lose its pairing.
  - serve closes with 4401 only for a token that `device.check` refuses, or for a revoked device.
- Then the server sends `{"state":{"seq","workspaces","tasks","worktrees","sessions","limits","queue","sends"}}`. It sends one `{"diff":{...}}` for each change to these items.
  - A diff sets `seq` and one of `workspace`, `task`, `worktree`, `session`, `limits` (whole list), `queue` (whole list), `sends` (whole list), `removed_workspace`, `removed_worktree`, `removed_session`.
  - serve drops events, subagents, review drafts and comments, so `seq` has gaps. Worktrees come without `Ports`.
- Each stream keeps a `view.View` ([internal/view/](../view/AGENTS.md)) of tasks, worktrees, sessions and their last `domain.SessionEventsKept` events. Thus serve sends a session as a `StreamSession`: the Go fields and these fields:
  - `name`: `domain.NameFor` with the PRs of its worktrees.
  - `where`: `domain.WorktreeLabel`, `repo@branch +N`.
  - `banner`: the body of `domain.BannerFor` for its state, also for a muted session. It is empty while the session is running or idle.
  - `since`: `domain.StateSince`, the time when the replayed events last changed its state. It is `null` when the events do not explain the state.

  The phone shows these fields and never calculates them again. If a task or worktree diff changes the derived fields of a different session, a `session` diff with the same `seq` follows it.
- `limits` is `domain.Quotas` of the sessions as `StreamQuota`: the Go fields and `label` (`domain.WindowLabel`), `low` and `stale_at` (`ReportedAt` + `domain.StaleQuotaAfter`).
  - It comes in a diff only when a session change or removal changes it.
  - Like the limits bar of the TUI, the client hides windows whose `ResetsAt` (Unix seconds, 0 unknown) has passed. It dims the windows after `stale_at`.
- `{"watch":"<session>","after":<cursor>}` adds the messages of a session through `transcript.watch` on the connection of the stream. `after` is the largest `cursor` that the client has, or 0 for all.
  - The first frame is `{"transcript":{"session","messages":[...]}}` with the messages after the cursor. Then one frame comes for each change.
  - `"reset":true` means that the session moved to a different transcript file. Drop its cursor. The messages that follow start the new file.
  - `"closed":true` means that the session is gone and the watch ended.
  - A new watch of the same session replaces its watch. A stream can have a maximum of 32 watches.
- `{"unwatch":"<session>"}` ends a watch. When the stream closes, all of its watches end.
- `{"visible":bool}` tells if the page is on screen.
  - serve passes it to `device.viewing` on the daemon connection of the stream, so it ends with the stream.
  - While a device is visible, the daemon sends no pushes to it (see "Web Push" in [internal/daemon/](../daemon/AGENTS.md)).
  - There is no reply. serve ignores a failed call. The only result is that the device still gets pushes.
- A failed watch gets `{"error":{"code","message"},"watch":"<session>"}`.
- An unknown client frame gets `{"error":{"code":"bad_request","message"}}`, and the stream stays open.
- Close codes:

  | Code | Meaning | Client |
  |---|---|---|
  | 4401 `CloseUnauthorized` | the daemon refused the token, or the device was revoked | confirms with an authed GET before it forgets the login (see [web/](../../web/AGENTS.md)) |
  | 4400 `CloseBadHandshake` | the first frame was late, not JSON, or had no token | retries |
  | 1013 | the daemon went away, or could not be reached to check the token | retries |
  | 1001 | serve is stopping (also while a stream waits for its token) | retries |
  | 1011 | a write failed | retries |
  | 1000 | the client left | none |
- Revocation:
  - A stream subscribes before it runs `device.check` on the same connection. Thus the loop of the daemon puts them in order. A revoke before the check makes the check fail. A revoke after the check gets to the subscription of the stream as a `revoked_device` diff.
  - That diff, or the diff on the `Start` connection, cancels all open streams of that device (4401 immediately). It also records the ID, so REST checks that answered immediately before it still fail.
  - The stream does not depend on the `Start` connection, which can be connecting again after a daemon restart.
  - The stream reads its subscription in its own goroutine and queues the diffs for the writer. Thus a phone that stops reading (a write blocked for up to 10 s) still sees its revocation immediately. The canceled write closes the socket, with 4401 if the transport is still open.
