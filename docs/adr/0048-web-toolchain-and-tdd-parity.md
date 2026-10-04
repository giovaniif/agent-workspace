# ADR 0048: The web app's toolchain, comment lint and tdd-check parity

Status: accepted, 2026-10-03.

## Context

ADR 0046 puts a PWA in `web/`, built into `internal/serve/dist` and embedded in the binary. It left two things open: how `scripts/tdd-check` treats web tests, and how `scripts/lint-comments` covers TypeScript. The owner settled the first: full parity with Go.

## Decision

### Toolchain

- React 19, TypeScript, Vite, Vitest with Testing Library and jsdom. npm with a committed `web/package-lock.json`. Building the UI needs Node 22.18 or newer (the repo's scripts in `web/scripts/` run as TypeScript through Node's type stripping); running `agentws` needs no Node.
- Fonts (IBM Plex Sans, JetBrains Mono) come from `@fontsource` packages and are bundled as hashed assets, Latin subsets only. Nothing loads from a CDN at runtime. Colors are the TUI's Latte palette as CSS variables in `web/src/theme.css`.

### Build and embedding

- `make web` runs `npm ci`, removes the previous build from `internal/serve/dist` (all but `index.html`) and runs `npm run build` (type check, then `vite build`).
- The build writes its HTML shell as `dist/app.html`, not `index.html`. The committed placeholder `dist/index.html` is never overwritten, so a build leaves the tree clean and `go build` works without Node. Built files are ignored by git.
- `serve.Static(fsys)` serves `app.html` (or the placeholder when there is no build) for `/` and for any path without an extension that is not a file (client routes), `no-cache`. Files under `assets/` are hashed and cached for a year as `immutable`; every other file (`sw.js`, the manifest, icons) is `no-cache`. `/api` and below answer 404, so a missing API route never returns the app. `serve.Dist()` is the embedded tree.
- goreleaser runs `make web` as a `before` hook, so release binaries always carry the app. CI runs `make web`, `npm test` and `AGENTWS_WEB_BUILT=1 go test ./internal/serve/ -run Static`, whose last test checks that the embedded build is served.

### Service worker and builds

- The app registers `/sw.js?build=<build>`, with the build `hello` reports. The worker caches into `agentws-shell-<build>`: on install it precaches `/` and the built files (the list is written into `sw.js` at build time), on activate it deletes other builds' caches and claims open pages. It answers same-origin `GET`s outside `/api` cache first, and navigations with the cached `/`.
- The build a page runs is the `build` in its controlling worker's script URL. On start, when the page becomes visible and every 5 minutes, the app compares it with `hello`. Different: it installs the worker for the new build, waits for it to activate, and reloads. No controller (a first load): it installs the worker and does not reload.

### Install and pairing

- The manifest has `display: standalone` and no `start_url`, so a Home Screen app added from the pair URL opens on that URL and finds its `#pair=` code. iOS gets `apple-touch-icon.png` and the `apple-mobile-web-app-*` tags.
- In a browser tab (neither `display-mode: standalone` nor `navigator.standalone`) the app shows how to add it to the Home Screen, with the code. In the installed app it shows the server's address, API version and build from `hello`, takes the code (prefilled, normalized to the pairing alphabet) and a device name, and calls `POST /api/v1/pair`. The token and device are stored in `localStorage` under `agentws.auth`. `401 unauthorized` says the code is wrong or expired, `429 rate_limited` says to wait a minute, and an unreachable server says so.

### Comments

- `scripts/lint-comments` covers `.ts`, `.tsx`, `.mts`, `.cts`, `.js`, `.jsx`, `.mjs`, `.cjs` and `.css`. In scripts, `//` and `/* */` are comments; in CSS, `/* */`. Strings, template literals (with nested `${}`) and regex literals are skipped.
- The one directive allowed is a TypeScript triple-slash `/// <reference ...>`, which the compiler reads. The app uses none today (`tsconfig.json` lists its `types`), but it is a compiler instruction, like `//go:`, not prose.
- It skips every `dist/` directory, so built output under `internal/serve/dist` is not linted.
- The scanner does not parse JSX text: `//` in text between tags reads as a comment. Put such text in a string expression (`{"https://..."}`).

### tdd-check parity

- `web/**/*.test.ts` and `web/**/*.test.tsx` are tests for the commit-order rule (ADR 0043), and so are files under `web/**/testdata/`.
- Helpers under `web/src/test/` are test code, so changing them with production code is not a mixed commit, but on their own they are not tests that must fail on base, like Go port fakes (ADR 0009).
- On base, `tdd-check` runs `npm ci` in `web/`, then `npx vitest run <path>` once per changed test file, or once for the directory that owns a changed `testdata/` file. Any run that passes fails the job, as a passing Go package does. A base without `web/package.json` cannot pass any web test, so they count as failing, like a compile error. A failed `npm ci` fails the job instead of counting as a failing test.
- `TDD_WEB_INSTALL` and `TDD_WEB_TEST` replace the two commands; `scripts/tddcheck` uses them to run the script without Node.

### Screenshots

- `scripts/web-screenshot` starts the Vite dev server with a fake API (`web/fake-api.ts`) and drives Playwright's Chromium at an iPhone viewport from steps on stdin. PWA PRs use it for their screenshots, pushed to `pr-assets`.

## Why

- **Per test file on base** is the closest match to Go's per-package run: running the whole suite on base would pass on every unchanged test and fail on nothing useful, and one run per file names the test that does not cover new behavior.
- **`app.html` beside a committed `index.html`** keeps `go build` working from a fresh clone and keeps `make web` from dirtying a tracked file.
- **A hand-written worker keyed by `hello`'s build** ties the cache to the binary that serves it, which is what ADR 0046 asks: a cached UI never talks to an API newer than it knows.
- **A Go scanner for TypeScript comments** keeps `make lint` free of Node, like the rest of the lint.

## Rejected

- **Web tests outside the tdd job:** the owner chose parity.
- **ESLint's comment rules:** `make lint` would need Node and `npm ci`.
- **vite-plugin-pwa or Workbox:** more dependencies for a cache this app can key by build in a few lines.
- **Google Fonts or another CDN:** the app must work on a private network, and the binary should carry everything it serves.
- **A `start_url` in the manifest:** the installed app would open on it and lose the `#pair=` code it was added with.

## Consequences

- CI's build, tdd and release jobs install Node 24.
- A PR that changes web tests makes the tdd job run `npm ci` on base.
- A test file that only moves helpers into `web/src/test/` and passes on base still fails the job; keep such refactors in a `test:` PR (ADR 0033).
