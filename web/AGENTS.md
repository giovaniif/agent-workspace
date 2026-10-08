# web

The phone app: a PWA that `agentws serve` embeds and serves at `/` (ADR [0046](../docs/adr/0046-remote-app.md)). The toolchain and decisions are in ADR [0048](../docs/adr/0048-web-toolchain-and-tdd-parity.md). The repo-wide rules in the [root AGENTS.md](../AGENTS.md) also apply here, for example no comments and test first.

## Commands

- Run `npm ci` one time. Then run `npm test` (Vitest, jsdom) and `npm run typecheck`.
- `make web` (from the repo root) builds into `internal/serve/dist`. It writes the shell as `app.html`. The committed `index.html` in that dir is the placeholder that a `go build` without Node embeds. Then `make build` gives a binary that serves the app.
- `npm run dev` runs Vite.
  - `AGENTWS_API=http://127.0.0.1:7420 npm run dev` proxies `/api` (and the stream) to a running `agentws serve`.
  - Add `AGENTWS_API_INSECURE=1` to accept its `--self-signed` certificate.
  - `npm run fake` uses the fake API in `fake-api.ts` instead.
  - The dev server binds all interfaces and accepts `.ts.net` hosts.
- `npm run icons` redraws the PNG icons in `public/` from `public/icon.svg` with Playwright.
- `scripts/web-screenshot` (see [scripts/](../scripts/AGENTS.md)) takes the PR screenshots.

## Layout

- `src/api.ts`: the HTTP client. Each error is an `ApiError` with the HTTP status and a code. The code is one of these:
  - the server's `error.code`
  - else, a code from the status (`401` → `unauthorized`, `429` → `rate_limited`)
  - `network`, when the client cannot get to the server.
- `src/App.tsx` selects the screen:
  - the session list, when `localStorage` holds `agentws.auth`
  - Add to Home Screen, in a browser tab
  - pairing, in the installed app.

  `AppEnv` holds all inputs from the browser (fetch, storage, display mode, URL hash, host, user agent), so tests can give fakes.
- `src/build.ts` and `src/sw.ts`/`src/sw-cache.ts`: the app registers the worker as `/sw.js?build=<hello build>`. The worker caches into `agentws-shell-<build>`. If the worker build of a page is not the build of `hello`, `checkBuild` reloads the page. `vite.config.ts` writes the built file list into `sw.js`.
- `src/stream.ts`: the `/api/v1/stream` client and its types. The types use the Go field names that the goldens pin. `StreamClient`:
  - sends the token first and applies diffs (`applyDiff`)
  - backs off from 1 s, doubling to 30 s. The backoff resets when a state arrives.
  - on close code 4401, asks `confirmRefused` before it stops. The app passes `tokenRefused`, an authed `GET /api/v1/workspaces`. Only a 401 `unauthorized` from that call makes the status `unauthorized`. All other results (success, 5xx, a network error) retry with the same token, as for all other closes.
  - retries on all other close codes, also 4400 (a late or malformed first frame).

  The app forgets `agentws.auth` only on `unauthorized` (or Sign out). Then it shows pairing, with a note that the device was removed or its login was rejected. Tests: `npm test -- stream SessionList`.
  - `onFrame` passes all frames (transcripts, watch errors). `send` writes `watch`/`unwatch` frames while `live`.
  - With `visible` set (`AppEnv.visibility`, `document.visibilityState` in `main.tsx`), it sends `{"visible":bool}` when live and on each `visibilitychange` (`visibilityChanged`). While the app is shown, it sends `{"visible":true}` every 30 s. Thus the daemon does not push to a phone that has the app open.
  - `useStreamClient` owns one client for each token. `useStream` reads its `StreamSnapshot` (`status`, `state`, `retryAt`).
  - The app holds one client in `App` and shares it with all screens. When the status goes back to `live`, a screen that watches a session sends its `watch` again.
- `src/api.ts` also has the authed session calls (`Authorization: Bearer <token>`): `messagesPage`, `sendMessage`, `unsend`, `interrupt`.
- `src/transcript.ts` keeps the messages of one session.
  - Pages and `transcript` frames merge by `id`, in `cursor` order. A later copy replaces the earlier copy in place, so a finished tool call updates its row.
  - A `reset` starts again and makes `before` unknown (`null`). Thus the next load of older messages asks for the newest page.
  - `toolRow` splits the tool name from `Tool.Summary`. `testResult` reads a go test, Vitest/Jest, pytest or cargo summary from the output.
  - A `Message` has no diff counts, so tool rows do not show them.
- `src/screens/Session.tsx` is the chat. It has the header, the log and the composer.
  - The header shows the name, `where`, harness and model, context left and a state chip. While the session runs or asks permission, it also shows Interrupt, behind a confirmation.
  - The log loads the newest page (50). Then it sends `watch` with the newest cursor it holds. It sends `watch` again each time the stream is `live` again. It sends `unwatch` when it closes.
  - A scroll near the top (or the button of the top row) loads the page before `before`. To keep the reading position, the log adds the height that it grew to `scrollTop`, because iOS Safari has no `overflow-anchor`. While the top row says loading, it keeps one height, so it never moves the log.
  - New messages scroll to the bottom only while the reader is at the bottom.
  - The composer sends with `POST …/messages`. While the session runs or asks permission, the button says Queue.
  - The queued sends of the session (stream `sends`) are above the composer. Each has Edit (unsend, then the text goes back into the composer) and Drop (unsend). If a send already went out (`not_found`), the app tells you.
  - The end of the log (`Chat`'s `children`, `.chat-end`) holds the permission card (`PermissionCard`). While the session state is `permission`, the card fetches `GET …/prompt`. It fetches again each time `since` changes, that is, when the session enters permission again. It shows the text with one button for each choice (`POST …/answer {choice}`).
  - A 409 `stale`, a `not_found`, or a stream that leaves `permission` changes the card to "This prompt is gone", and nothing is sent. A prompt with `raw` and no choices shows "Couldn’t read this dialog" and the raw pane text, with no buttons.
  - While the state is `permission`, the composer is disabled. Tests: `npm test -- Permission`.
- `src/sessions.ts`: the groups by attention, the time in state, and the limits rows. The groups are:
  - Needs you: permission, then waiting, longest first.
  - Working.
  - Done: unread first.
  - Idle.

  Ended sessions are not shown.
- `src/route.ts`:
  - `#/sessions/<id>` opens a session. Push notifications use the same URL.
  - `#/new` is the new session form (`screens/NewSession.tsx`). Its fields are workspace, work item, first prompt, agent, model and effort. After 350 ms, the form checks the work item through `/work-items/resolve`. It keeps the choices in `agentws.new` in `localStorage`. The default workspace is the last one that the daemon used. A launch error, also the output of a setup recipe, shows in the form. A success opens `#/sessions/<id>`.
  - All other routes show the list.
- `src/push.ts` and `src/sw-push.ts`: Web Push. `AppEnv.push` (`browserPush(window)` in `main.tsx`, `FakePush` in tests) wraps `Notification` and `pushManager`.
  - When Settings opens, it reads the real state. If permission is granted, it calls `PushEnv.subscribed()`: the `pushManager` of the worker registration holds a subscription. Then it shows "Notifications are on for this device" with Turn off (`disablePush`), not Enable. It does not ask the server, because the server has no endpoint that reports the subscription of a device.
  - `enablePush` runs only from the Settings button. It asks for permission first, because iOS needs a tap from the user for this. Then it calls `GET /api/v1/push/key`, subscribes with that key, and posts the subscription to `/api/v1/push/subscribe`.
  - The `push` handler of the worker shows `{"title","body"}`, tagged by session. It never pushes silently: if a field is missing, it uses `agentws` / `needs you`.
  - `notificationclick` focuses an open window of the app and navigates it to the `url` of the push (`/#/sessions/<id>`, same-origin paths only). If no window is open, it opens one.
  - Settings also shows static text: pushes are held while the owner types at the terminal, and are not sent while the app is on screen.
  - Sign out first calls `POST /api/v1/push/unsubscribe` and unsubscribes the browser (best effort). Then it forgets the token.

  Tests: `npm test -- Push` and `npm test -- Settings`.
- `#/settings` shows Settings (server, this device, Enable notifications, sign out). A tab bar under the session list and Settings switches between them.
- `src/screens/`: one component for each screen. `src/theme.css`: the Latte palette of the TUI as CSS variables, and IBM Plex Sans and JetBrains Mono from `@fontsource`.
- `src/test/`: test helpers (`FakeServer`, `MemoryStorage`, the Vitest setup). tdd-check treats them as port fakes.

## Tests

- Text colors must have a contrast of 4.5:1 or more on their background. Tap targets must be 44 px or more. `src/theme.test.ts` checks the token pairs and the `min-height` of the tappable classes. When you use a new pair or class, add it there.
- Test through the rendered screen with Testing Library (roles, labels, text), and through module inputs and outputs. Use `FakeServer` for the network. Never mock modules.
- For tdd-check, `*.test.ts(x)` files count as Go tests do. Commit them before the code. They must fail on base.
