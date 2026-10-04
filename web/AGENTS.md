# web

The phone app: a PWA that `agentws serve` embeds and serves at `/` (ADR [0046](../docs/adr/0046-remote-app.md), toolchain and decisions in ADR [0048](../docs/adr/0048-web-toolchain-and-tdd-parity.md)). Repo-wide rules are in the [root AGENTS.md](../AGENTS.md); they apply here too, including no comments and test first.

## Commands

- `npm ci` once, then `npm test` (Vitest, jsdom) and `npm run typecheck`.
- `make web` (from the repo root) builds into `internal/serve/dist`. The shell is written as `app.html`; the committed `index.html` there is the placeholder a Node-less `go build` embeds. Then `make build` gives a binary that serves the app.
- `npm run dev` runs Vite; `AGENTWS_API=http://127.0.0.1:7420 npm run dev` proxies `/api` (and the stream) to a running `agentws serve`; add `AGENTWS_API_INSECURE=1` to accept its `--self-signed` certificate. `npm run fake` uses the fake API in `fake-api.ts` instead. The dev server binds every interface and accepts `.ts.net` hosts.
- `npm run icons` redraws the PNG icons in `public/` from `public/icon.svg` with Playwright.
- `scripts/web-screenshot` (see [scripts/](../scripts/AGENTS.md)) takes the PR screenshots.

## Layout

- `src/api.ts`: the HTTP client. Every error is an `ApiError` with the HTTP status and a code: the server's `error.code`, else one derived from the status (`401` → `unauthorized`, `429` → `rate_limited`), `network` when the server cannot be reached.
- `src/App.tsx` picks the screen: the session list when `localStorage` holds `agentws.auth`, Add to Home Screen in a browser tab, pairing in the installed app. `AppEnv` carries everything from the browser (fetch, storage, display mode, URL hash, host, user agent), so tests pass fakes.
- `src/build.ts` and `src/sw.ts`/`src/sw-cache.ts`: the worker is registered as `/sw.js?build=<hello build>` and caches into `agentws-shell-<build>`; `checkBuild` reloads a page whose worker build differs from `hello`'s. `vite.config.ts` writes the built file list into `sw.js`.
- `src/screens/`: one component per screen. `src/theme.css`: the TUI's Latte palette as CSS variables, IBM Plex Sans and JetBrains Mono from `@fontsource`.
- `src/test/`: test helpers (`FakeServer`, `MemoryStorage`, the Vitest setup). tdd-check treats them like port fakes.

## Tests

- Test through the rendered screen with Testing Library (roles, labels, text) and through module inputs and outputs; fake the network with `FakeServer`, never mock modules.
- `*.test.ts(x)` files count for tdd-check like Go tests: commit them before the code, and they must fail on base.
