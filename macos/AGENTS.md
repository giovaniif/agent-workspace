# macos

The native Mac app ([ADR 0049](../docs/adr/0049-macos-app.md), UX in [docs/macos-app-design.md](../docs/macos-app-design.md)). Repo-wide rules are in the [root AGENTS.md](../AGENTS.md); they apply here too, including no comments and test first.

## Layout

- `Package.swift`: one SwiftPM package (Swift 6 tools, macOS 15).
  - `Sources/AgentwsKit`: the library (protocol types, transport, state, view model; no UI). It must build on Linux too, so it imports no AppKit or SwiftUI.
  - `Sources/AgentwsViews`: the SwiftUI views (`MainWindow`, `LiveWindow`, `Snapshot`), all inside `#if canImport(SwiftUI)`. Views draw a `WindowScene` (state, connection, selection, filter, inspector, clock) and call `WindowActions`, so a seeded scene renders without a daemon.
  - `Sources/AgentwsApp`: the app's executable target (SwiftUI, AppKit where needed). Its UI code sits inside `#if canImport(SwiftUI)` so `swift build` and `swift test` still work on Linux.
  - `Tests/AgentwsKitTests`: Swift Testing tests of `AgentwsKit`.
- `Info.plist` and `build-app`: `build-app [release|debug]` builds `AgentwsApp` and wraps it into `build/agentws.app` (bundle id `dev.agentws.app`), ad-hoc signed (`codesign -s -`). There is no Developer ID and no notarization. `AGENTWS_APP_VERSION` (default `v0.0.0`) is the release tag: its numeric part becomes `CFBundleShortVersionString` and the whole tag `AgentwsVersion`; `AGENTWS_APP_BUILD` (default `0`, the workflow run number in CI) is `CFBundleVersion`; `AGENTWS_APP_BINARIES` points at a directory with `<os>_<arch>/agentws` for `darwin_arm64`, `darwin_amd64`, `linux_amd64` and `linux_arm64`, copied into `Contents/Resources/bin/<os>_<arch>/agentws`. Without it the app has no bundled `agentws`, which is fine for local UI work.
- `build-dmg [out]`: wraps `build/agentws.app` and an `/Applications` link into an unsigned DMG (default `build/agentws.dmg`).

## Release

The `macos-app` job in `.github/workflows/release.yml` unpacks the goreleaser archives, runs `build-app` with the release tag as the version, checks the bundled darwin binary reports that tag, builds `agentws_<tag>.dmg` and uploads it to the GitHub release. On a pull request touching `macos/`, `.goreleaser.yaml` or the workflow, and on `workflow_dispatch`, goreleaser runs with `--snapshot` and the DMG is only a workflow artifact, so that is the dry run. There is no Apple Developer account, so the app is ad-hoc signed and not notarized (issue #235): Gatekeeper blocks the first launch until the user clears the quarantine flag (see the README).

## AgentwsKit

- Protocol (`Envelope.swift`, `ViewTypes.swift`): `Request` carries `v`, `id`, `method`, `params` and `build` (the handshake of ADR 0039, so the app's build must equal the server's). `RPCError.kind` maps the daemon's codes, `unavailable` and `version_mismatch` included. The `view.subscribe` types keep the Go field names as `CodingKeys`. A field Go may send as `null` uses `@Nullable` (an optional whose key must still be present) or `@NullAsEmpty` (a list), so a key renamed on either side fails to decode instead of reading as `nil`.
- Transport (`Transport.swift`): `Endpoint.local(binary:)` runs `<binary> rpc`; `Endpoint.ssh(host:remoteBinary:)` runs `ssh -T -o BatchMode=yes -o ServerAliveInterval=15 <host> <bin> rpc`, through `/usr/bin/env` so `ssh` comes from `PATH`. `LineProcess` speaks newline-delimited lines over the process's stdio and keeps stdin open until `close()`. `RPCClient` matches replies to calls by `id`; a reply with `id` 0 (the bridge's `unavailable` when no daemon listens) fails every call, and a dead process fails them with `AgentwsError.disconnected`.
- Live state (`LiveState.swift`, `ViewStore.swift`): `ViewState.apply(_:)` applies a diff as the TUI does (replace by key, removals, whole-list `limits`/`queue`/`sends`, the last 20 events per session). `ViewStore` (`@Observable`, main actor) keeps one process for `view.subscribe` and another for `call`, so diffs never delay a call. After a drop it waits by `Backoff` (1 s doubling to 30 s, reset by each new state) and resubscribes from scratch. `connection` is `live`, `connecting`, `reconnecting(in:)`, `unavailable(message)` (keeps retrying) or `versionMismatch(message)` (stops until `start()` again).
- Presentation (`StateStyle.swift`, `Sidebar.swift`, `Chrome.swift`): everything the main window shows, as plain values testable on Linux. `StateStyle` maps a state to glyph, Latte/Mocha tone and label (done's dot carries a check, so no two states differ by colour alone). `Sidebar` keeps the daemon's `order`, numbers the first nine rows ⌘1–⌘9 and puts ended sessions in their own group; `Navigator` implements ⌘1–9, ⌃Space (next session that needs you, wrapping), ⌘[ and reselection after a removal. `Header`, `QuotaMeter`, `Toolbar` and `ConnectionBanner` (`unavailable` reads "retrying", `version_mismatch` "stopped" with Reconnect) cover the rest. `Seed.window` is the fixed ten-session state the snapshots and `--demo` use.
- Build (`Build.swift`): `Build.read(binary:)` runs `<binary> version --build`, the exact string the daemon compares in the handshake. The app reads it from the `agentws` it talks to: `$AGENTWS_BINARY`, else `Contents/MacOS/agentws-cli` in the bundle, else `agentws` on `PATH`. If that fails it sends `unknown`, and the daemon's `version_mismatch` shows the stopped banner.
- Tests decode `internal/view/testdata/view-subscribe-*.json` (the Go goldens) and check that every non-null field round-trips. Transport tests put fake `agentws` and `ssh` scripts on `PATH`; the fake `ssh` blocks on `/dev/tty` unless it gets `BatchMode=yes`.

## Why no Xcode project

The app target is a SwiftPM executable plus `build-app`, not an `.xcodeproj`. Everything is plain text that can be edited and reviewed on Linux, with no generated project to keep in sync and no XcodeGen step. `swift test` runs `AgentwsKit`'s tests anywhere a Swift toolchain exists; only the app bundle needs macOS. To use Xcode, open `Package.swift` (File > Open); Xcode reads the package directly.

## Commands

- `swift test` (in `macos/`): the `AgentwsKit` tests. On Linux, install a toolchain with [swiftly](https://www.swift.org/install/linux/) (`swiftly install latest`).
- `swift test --filter AgentwsKitTests.ProtocolTests`: one suite.
- `macos/build-app` (macOS only): `macos/build/agentws.app`; `open macos/build/agentws.app` runs it, and `open macos/build/agentws.app --args --demo` shows the seeded window without a daemon.
- CI's `macos` job runs both on `macos-15`.

## Tests

- Name each test file after the one suite type it holds (`ProtocolTests.swift` holds `struct ProtocolTests`). `scripts/tdd-check` runs `swift test --filter <Target>.<File>` on base for every changed test file, and a filter that matches nothing exits 0, which counts as passing on base.
- Test files count for tdd-check like Go tests: commit them before the code, and they must fail on base (a compile error counts). A base without `macos/Package.swift` counts as failing.
- Snapshots: `SnapshotTests` (macOS only, `#if canImport(AppKit)`) renders seeded `WindowScene`s through `NSHostingView` into PNGs under `$AGENTWS_SNAPSHOTS` (default `$TMPDIR/agentws-snapshots`) and checks the state colours appear. CI uploads them as the `macos-snapshots` artifact; a UI PR embeds them from the `pr-assets` branch under `<issue>/`. They are not compared to stored references, since a runner image change shifts antialiasing.
- `scripts/lint-comments` covers Swift: no `//`, `///` or `/* */` comments. Only the `// swift-tools-version:` line of `Package.swift` passes.
