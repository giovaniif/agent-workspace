# ADR 0051: A browser in the Mac app that agents control

Status: accepted, 2026-10-13. Epic: #330. Work items: #336, #337, #338.

## Context

Agents on the server change web apps. They cannot see the result. The owner wants a browser that agents control, like the browser of Orca. The owner views everything from a Mac. The server is headless and has no display.

These facts apply:

- The Mac app connects to the daemon through `agentws rpc` over SSH (ADR 0049). The connection is open while the app runs.
- A dev server on the server is available on the tailnet at `host.<tailnet>.ts.net:<port>`. The daemon knows the TCP ports of each worktree (ADR 0022).
- The logins of the owner are in the browsers of the Mac: Chrome, Arc, Brave, Edge and Safari.

## Decision

### The browser is in the Mac app

- The Mac app has a **Browser** view for each session, next to Terminal, Review, Shell and nvim. It uses `WKWebView`.
- The view has tabs, an address bar, back, forward, reload and device sizes (iPhone, iPad, desktop).
- A browser profile is one `WKWebsiteDataStore` (cookies and local storage). Each session uses one profile. The app keeps the profiles on the Mac.
- The TCP ports that the daemon knows for the worktrees of the session show as quick links to `http://<server host>:<port>/`.

### Agents control it through the daemon

1. An agent runs `agentws browser <command>` in its pane, for example `goto`, `snapshot`, `click` or `fill`.
2. The CLI sends `browser.command` to the daemon on its socket. The CLI finds the session from its pane.
3. The daemon sends the command to the Mac app that is attached as the browser.
4. The app runs the command in the tab of that session. Then it sends the result back on the same connection.
5. The daemon gives the result to the CLI. The CLI prints it, or prints JSON with `--json`.

- The app attaches with `browser.attach` on its `agentws rpc` connection. The daemon keeps one browser connection. A new attach replaces the old one. The attach ends when the connection closes.
- The daemon does not keep browser state. It only sends commands and results. Each command has a timeout.
- `snapshot` returns an accessibility tree with element refs (`@e1`). The refs are valid until the next `snapshot`.
- `screenshot` writes a PNG file into the worktree or into a temporary folder on the server. Thus the agent can read it.
- While an agent controls a tab, the app shows a banner on it. The user can take control at any time.

### Where an agent can go

The Mac is on networks that the server cannot reach. An agent must not use the browser to read services on the Mac or its local network. Thus the app applies this policy to each navigation while an agent controls the tab, also to each redirect and each frame (`WKNavigationDelegate`):

- Only `http` and `https` are allowed.
- The server host of the session is allowed, on all TCP ports. For This Mac, this is `localhost`. Thus dev servers open.
- Other hosts are allowed only when they resolve to public addresses. Loopback, private, link-local, CGNAT (`100.64.0.0/10`) and multicast addresses are refused.
- A refused navigation stops, and the command fails with the reason. The app does not follow the redirect.
- When the user takes over, the policy stops for that tab until the next agent command. The user can open each address.

### No Mac app means no browser

- If no app is attached, `browser.command` fails immediately with `unavailable` and the message `no browser: the macOS app is not connected`.
- The daemon does not start a headless browser on the server.

### Cookies stay on the Mac

- The app imports cookies from Chrome, Arc, Brave and Edge (the SQLite store and the Safe Storage key from the Keychain), from Safari (`Cookies.binarycookies`), or from a file (Netscape `cookies.txt`, a JSON cookie list or Playwright `storageState`).
- The app imports into one browser profile. A new import replaces the cookies of those domains.
- Cookies never go to the server or into a log. The `cookies` command of the CLI shows names and domains, not values.
- The parsers are in `AgentwsKit`, with tests on fixture files.

## Why

- The Mac has a display, a real browser engine and the logins of the owner. The server has none of these.
- The connection of the app already exists and goes through SSH. Thus the browser needs no new listener, token or TCP port.
- The daemon only sends messages between two connections. This keeps the event loop fast and keeps the rules simple.
- The user sees what the agent does, in the same window as the session.

## Rejected

- **A headless Chromium on the server (Playwright or CDP):** the user cannot see it, it uses much memory, and the logins of the user are not on the server.
- **A browser extension on the Mac:** it needs a second connection and a second install, and it cannot show the browser next to the session.
- **Cookies on the server:** agents share the server, and cookies give access to the accounts of the user.

## Consequences

- The browser works only while the Mac app runs and is connected. Agents must accept the `unavailable` error.
- `browser.attach`, `browser.command` and the `agentws browser` CLI are new public surface.
- One round trip of a browser command has a latency budget in ARCHITECTURE.md (#337).
- A cookie import from a Chrome-type browser needs the approval of the user for the Keychain. An import from Safari needs Full Disk Access.
