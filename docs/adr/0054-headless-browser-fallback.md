# ADR 0054: A headless browser on the server when no Mac app is connected

Status: accepted, 2026-10-14. Epic: #330. Work item: #361. This ADR changes the "No Mac app means no browser" and "Cookies stay on the Mac" parts of [ADR 0051](0051-browser-in-mac-app.md).

## Context

The agent browser is in the macOS app (ADR 0051). When no Mac app is connected, `crew browser` fails with `no browser: the macOS app is not connected`. The Mac can sleep at night. Thus a scheduled automation that needs a browser cannot run without the user.

## Decision

### A second browser backend

- The daemon can start a headless Chromium on the server. It controls Chromium over the Chrome DevTools Protocol (CDP) on a WebSocket.
- The daemon runs the Chromium binary that is installed. It does not use a Go browser library. The CDP client uses the WebSocket library that `crew serve` already uses.
- The headless backend accepts the same commands as the Mac app. It runs the same snapshot script, so the snapshot text and the `@eN` refs are the same.
- The headless backend has one tab for each session.

### Which backend runs a command

The pure rule is `domain.ChooseBrowser`:

| `browser` of the automation | Mac app connected | No Mac app |
|---|---|---|
| `any` (default) | Mac app | headless if `[browser] fallback = "headless"`, else the error of ADR 0051 |
| `mac` | Mac app | the error of ADR 0051 |
| `headless` | headless | headless |

- A session that an automation did not start uses `any`.
- `[browser] fallback` is `off` by default.

### Resources

- One Chromium for each session, at most. `[browser] max_headless` (default 2) is the limit for all sessions. When the limit is full, a command for a new session fails with a clear error.
- The daemon stops a Chromium after `[browser] idle` (default `10m`) with no command. It also stops it when the session ends.
- Commands run on the goroutine of the connection, never on the event loop. Each command has the timeout of ADR 0051.
- Each Chromium has a temporary user data folder. The daemon removes it when Chromium stops.

### Cookies on the server

- `crew browser cookies import <file>` reads Netscape `cookies.txt`, a JSON cookie list or Playwright `storageState`. The user runs it on purpose.
- The daemon keeps the cookies in `$CREW_HOME/browser/profiles/default/cookies.json`, mode 0600. A new import replaces cookies with the same name, domain and path.
- Each new headless Chromium gets these cookies. A Chromium that runs gets them at the time of the import.
- The `cookies` command shows names and domains, not values, as in ADR 0051.

### No navigation policy

The Mac app refuses private addresses because the Mac is on networks that the server cannot reach. The headless Chromium runs on the server. It can reach only what the agents on the server can already reach with `curl`. Thus the headless backend does not refuse private addresses.

## Why

- A scheduled automation must finish when the Mac sleeps.
- Chromium is a common package on Linux. CDP is a stable, documented protocol.
- `off` by default keeps the behaviour of ADR 0051 until the user selects the fallback.

## Rejected

- **chromedp or Playwright:** a large dependency for a small set of commands. Playwright also needs Node.
- **One shared Chromium for all sessions:** one crash stops all sessions, and the memory of one session is not released when it ends.
- **A persistent Chromium profile folder:** Chromium locks its profile folder, so two sessions cannot share it.

## Consequences

- One headless Chromium has a memory budget in ARCHITECTURE.md.
- The user must install Chromium on the server. `crew setup claude` and `crew setup codex` tell the user if it is not installed.
- Imported cookies are on the server. Only the user who owns `$CREW_HOME` can read them.
- Not in this ADR: the "send profile to server" action of the Mac app and the live screenshot stream of a headless tab in the Mac app. They are follow-up work.
