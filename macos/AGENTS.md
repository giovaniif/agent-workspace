# macos

The native Mac app ([ADR 0049](../docs/adr/0049-macos-app.md), UX in [docs/macos-app-design.md](../docs/macos-app-design.md)). Repo-wide rules are in the [root AGENTS.md](../AGENTS.md); they apply here too, including no comments and test first.

## Layout

- `Package.swift`: one SwiftPM package (Swift 6 tools, macOS 15).
  - `Sources/AgentwsKit`: the library (protocol types, transport, state, view model; no UI). It must build on Linux too, so it imports no AppKit or SwiftUI.
  - `Sources/AgentwsApp`: the app's executable target (SwiftUI, AppKit where needed). Its UI code sits inside `#if canImport(SwiftUI)` so `swift build` and `swift test` still work on Linux.
  - `Tests/AgentwsKitTests`: Swift Testing tests of `AgentwsKit`.
- `Info.plist` and `build-app`: `build-app [release|debug]` builds `AgentwsApp` and wraps it into `build/agentws.app` (bundle id `dev.agentws.app`), ad-hoc signed (`codesign -s -`). There is no Developer ID and no notarization.

## Why no Xcode project

The app target is a SwiftPM executable plus `build-app`, not an `.xcodeproj`. Everything is plain text that can be edited and reviewed on Linux, with no generated project to keep in sync and no XcodeGen step. `swift test` runs `AgentwsKit`'s tests anywhere a Swift toolchain exists; only the app bundle needs macOS. To use Xcode, open `Package.swift` (File > Open); Xcode reads the package directly.

## Commands

- `swift test` (in `macos/`): the `AgentwsKit` tests. On Linux, install a toolchain with [swiftly](https://www.swift.org/install/linux/) (`swiftly install latest`).
- `swift test --filter AgentwsKitTests.ProtocolTests`: one suite.
- `macos/build-app` (macOS only): `macos/build/agentws.app`; `open macos/build/agentws.app` runs it.
- CI's `macos` job runs both on `macos-15`.

## Tests

- Name each test file after the one suite type it holds (`ProtocolTests.swift` holds `struct ProtocolTests`). `scripts/tdd-check` runs `swift test --filter <Target>.<File>` on base for every changed test file, and a filter that matches nothing exits 0, which counts as passing on base.
- Test files count for tdd-check like Go tests: commit them before the code, and they must fail on base (a compile error counts). A base without `macos/Package.swift` counts as failing.
- `scripts/lint-comments` covers Swift: no `//`, `///` or `/* */` comments. Only the `// swift-tools-version:` line of `Package.swift` passes.
