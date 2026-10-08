# macos

The native Mac app ([ADR 0049](../docs/adr/0049-macos-app.md), UX in [docs/macos-app-design.md](../docs/macos-app-design.md)). The repo-wide rules in the [root AGENTS.md](../AGENTS.md) also apply here, for example no comments and test first.

## Layout

- `Package.swift`: one SwiftPM package (Swift 6 tools, macOS 15).
  - `Sources/AgentwsKit`: the library (protocol types, transport, state, view model; no UI). It must also build on Linux, so it does not import AppKit or SwiftUI.
  - `Sources/AgentwsViews`: the SwiftUI views (`MainWindow`, `LiveWindow`, `ReviewView`, `Snapshot`), all inside `#if canImport(SwiftUI)`.
    - Views draw a `WindowScene` and call `WindowActions`. Thus a seeded scene renders without a daemon. A `WindowScene` holds state, connection, selection, filter, inspector, clock, and `review` while review mode is on.
    - Settings work the same way: `SettingsView` draws a `SettingsScene` and calls `SettingsActions`. `LiveSettings` connects them to `SettingsStore` and `ServerSettings`.
  - `Sources/AgentwsApp`: the executable target of the app (SwiftUI, and AppKit where necessary). Its UI code is inside `#if canImport(SwiftUI)`, so `swift build` and `swift test` still work on Linux.
  - `Tests/AgentwsKitTests`: Swift Testing tests of `AgentwsKit`.
- `Info.plist` and `build-app`: `build-app [release|debug]` builds `AgentwsApp` and wraps it into `build/agentws.app` (bundle id `dev.agentws.app`). It signs the app ad-hoc (`codesign -s -`). There is no Developer ID and no notarization. These environment variables apply:
  - `AGENTWS_APP_VERSION` (default `v0.0.0`) is the release tag. Its numeric part becomes `CFBundleShortVersionString`, and the full tag becomes `AgentwsVersion`.
  - `AGENTWS_APP_BUILD` (default `0`, the workflow run number in CI) is `CFBundleVersion`.
  - `AGENTWS_APP_BINARIES` points to a directory with `<os>_<arch>/agentws` for `darwin_arm64`, `darwin_amd64`, `linux_amd64` and `linux_arm64`. `build-app` copies them into `Contents/Resources/bin/<os>_<arch>/agentws`. Without it, the app has no bundled `agentws`. This is correct for local UI work.
- App icon: `AppIcon.png` (1024 px) comes from `web/public/icon.svg`. `npm run icons` in `web/` makes it again. `build-app` makes `Contents/Resources/AppIcon.icns` from it with `sips` and `iconutil` (16 px to 1024 px, with `@2x`). `CFBundleIconFile` in `Info.plist` points to it.
- `build-dmg [out]`: wraps `build/agentws.app` and an `/Applications` link into an unsigned DMG (default `build/agentws.dmg`). The DMG uses the same icon as its volume icon (`.VolumeIcon.icns`).

## Release

The `macos-app` job in `.github/workflows/release.yml` does these steps:

1. It unpacks the goreleaser archives.
2. It runs `build-app` with the release tag as the version.
3. It checks that the bundled darwin binary reports that tag.
4. It builds `agentws_<tag>.dmg` and uploads it to the GitHub release.

The dry run occurs on a pull request that touches `macos/`, `.goreleaser.yaml` or the workflow, and on `workflow_dispatch`. Then goreleaser runs with `--snapshot`, and the DMG is only a workflow artifact. There is no Apple Developer account, so the app is ad-hoc signed and not notarized (issue #235). Gatekeeper blocks the first launch until the user clears the quarantine flag (see the README).

## AgentwsKit

- Protocol (`Envelope.swift`, `ViewTypes.swift`):
  - `Request` carries `v`, `id`, `method`, `params` and `build`. `build` is the handshake of ADR 0039: the build of the app must be equal to the build of the server.
  - `RPCError.kind` maps the codes of the daemon, with `unavailable` and `version_mismatch`.
  - The `view.subscribe` types keep the Go field names as `CodingKeys`.
  - If Go can send a field as `null`, the field uses `@Nullable` (an optional whose key must still be present) or `@NullAsEmpty` (a list). Thus, if one side renames a key, decoding fails. It does not read the field as `nil`.
- Transport (`Transport.swift`):
  - `Endpoint.local(binary:)` runs `<binary> rpc`.
  - `Endpoint.ssh(host:remoteBinary:)` runs `ssh -T -o BatchMode=yes -o ServerAliveInterval=15 <host> <bin> rpc` through `/usr/bin/env`, so `ssh` comes from `PATH`.
  - `LineProcess` sends and receives newline-delimited lines on the stdio of the process. It keeps stdin open until `close()`.
  - `RPCClient` matches replies to calls by `id`. A reply with `id` 0 fails all calls. The bridge sends this reply (`unavailable`) when no daemon listens. If the process stops, all calls fail with `AgentwsError.disconnected`.
- Live state (`LiveState.swift`, `ViewStore.swift`):
  - `ViewState.apply(_:)` applies a diff as the TUI does: replace by key, removals, whole-list `limits`/`queue`/`sends`, and the last 20 events for each session.
  - `ViewStore` (`@Observable`, main actor) keeps one process for `view.subscribe` and a different one for `call`. Thus diffs never delay a call.
  - After a drop, it waits by `Backoff` (1 s, doubled each time to 30 s, reset by each new state). Then it subscribes again from the start.
  - `connection` is `live`, `connecting`, `reconnecting(in:)`, `unavailable(message)` (it continues to try) or `versionMismatch(message)` (it stops until `start()` runs again).
- Presentation (`StateStyle.swift`, `Sidebar.swift`, `Chrome.swift`): all that the main window shows, as plain values that you can test on Linux.
  - `StateStyle` maps a state to a glyph, a Latte/Mocha tone and a label. The dot of done has a check, so no two states are different only by colour.
  - `Sidebar` keeps the `order` of the daemon, gives the first nine rows the numbers ⌘1–⌘9, and puts ended sessions in their own group.
  - `Navigator` implements ⌘1–9, ⌃Space (the next session that needs you, with wrap), ⌘[ and a new selection after a removal.
  - `Header`, `QuotaMeter`, `Toolbar` and `ConnectionBanner` cover the rest. In `ConnectionBanner`, `unavailable` shows "retrying", and `version_mismatch` shows "stopped" with Reconnect.
  - `Seed.window` is the fixed ten-session state that the snapshots and `--demo` use.
- Disk window (`Disk.swift`, ⇧⌘W by default, screen 5):
  - `DiskView` decodes `disk.view`. `DiskRow` keeps the Go field names. A size of `-1` means that the measurement continues.
  - `DiskTiles` reads the totals that the daemon sends. While it measures sizes, it shows `…` or a trailing `+`.
  - `DiskTable` joins rows with the worktrees and sessions of `view.subscribe`. It gives the state (Open, Merged · clean, Merged · dirty, Detached, No PR) and what cleanup will do ("removes at 14:30" from `next_cleanup`, in local time).
  - The sidebar footer ends with `ViewState.reclaimable` ("1.2 GB reclaimable").
  - `DiskActions` lists the actions of the action bar, each with an outcome sentence. For a `backup_then_ask` row, Back up and remove is the only removal action. A `keep` row offers no removal. Each destructive action has a confirmation.
  - `DiskPorts` and `DiskRecent` fill the other two tabs.
  - `LiveDiskWindow` polls `disk.view` (every 2 s while sizes are pending, else every 10 s). It calls `cleanup.worktree` and `ports.kill`.
  - Go to session and Open shell go through `WindowRouter` to the main window. The main window selects the session and shows the shell of the worktree. The shell of a worktree with no owner has no session to show under, so it still calls `shell.toggle` and reports it.
  - `Seed.disk` is the seeded view for snapshots and `--demo`.
- New session (`NewSession.swift`, view `NewSessionSheet`):
  - ⌘N opens the sheet. ⇧⌘N opens its Linear launcher tab. The New button of the toolbar also opens it.
  - `NewSession.load(state:)` calls `session.options`. It gets the harnesses, their models and efforts, the `[defaults.<harness>]` model and effort, and `max_parallel`. The app cannot read a remote `config.toml`.
  - `NewSessionForm` preselects the workspace with the newest `LastUsed`. It labels each workspace `orchestration root · N repos` or `single repo`. When you select a harness, model and effort go back to its defaults.
  - 300 ms after the last edit, `session.resolve` resolves the work item into a `WorkItemCard`: source, title, branch, and the path of an existing worktree on that branch. The form ignores a reply for an older text.
  - `warning` reads the `limits` of the view: the lowest `low` quota of the selected harness. It shows a switch button only when a different harness has reported limits and none of them is low (the rule of `domain.OfferFallback`).
  - Start (⌘↩) calls `session.new`, then `session.focus`, and the window selects the new session. A failure, for example the output of a setup recipe, stays in the sheet.
  - The launcher sends `launcher.enqueue` and lists the `queue` of the view (`LaunchRow`).
  - Tests: `swift test --filter NewSession` with a fake `Caller`.
- Build (`Build.swift`): `Build.read(binary:)` runs `<binary> version --build`. The result is the exact string that the daemon compares in the handshake. The app reads it from the `agentws` that it talks to, in this order:
  1. `$AGENTWS_BINARY`
  2. the bundled `Contents/Resources/bin/darwin_<arch>/agentws` (`Build.binary`)
  3. `agentws` on `PATH`.

  If this fails, the app sends `unknown`, and the `version_mismatch` of the daemon shows the stopped banner.
- Review (`Review.swift`, `ReviewScreen.swift`, `ReviewController.swift`, `SeedReview.swift`):
  - The app calls `review.open` with `"tokens": true` and decodes it with the Go field names. The `Kind` of a line is the Go byte (32, 43, 45). The app sends it back as a byte in the `file` of `review.hunk`.
  - `Spans` are `[start, end, class]` byte offsets into `Text`. `Highlight.segments` cuts on UTF-8 bytes and ignores spans outside the line.
  - `ReviewScreen` is the plain value that the view draws:
    - the file tree, grouped by worktree with its PR
    - the worktree menu (All first, filtered locally)
    - `SplitRow.rows`
    - viewed ticks (a mark counts only while its blob matches)
    - comment ranges (`CommentAnchor` anchors as `domain.CommentOn` does, and shift-click extends with `extendComment`)
    - draft rows `repo · path:lines`, and the busy notice.
  - `ReviewController` (main actor, over the `Caller` protocol that `ViewStore` implements) runs `review.open`, `review.viewed`, `review.comment`, `review.send` (`{"session","note"}`), `review.hunk` and `nvim.open`. If a call fails, the review does not change, and the controller shows the message of the daemon. For a refused hunk, this is the message of the patch tool.
  - `ReviewTests` keeps the decode and model of a 50-file review under 300 ms.
- Terminals (`Control.swift`, `TerminalLink.swift`, `TerminalPanes.swift`, `TerminalHub.swift`; ADR 0049 "Terminals", spike #216):
  - `client.native` returns the control-mode argv, the session and all panes. Each pane has its size and the mouse/cursor/alternate modes, because tmux does not send them again to a new client.
  - `Endpoint.terminalArgv` runs the argv as it is locally, and as `ssh -T … <host> -- '<argv>'` remotely.
  - `ControlParser` changes lines into events: `%output` and `%extended-output` (unescaped byte by byte), `%begin`/`%end`/`%error` blocks, `%layout-change` leaf sizes, window add/close, `%pause`/`%continue` and `%exit`.
  - `ReplyMatcher` binds each call to the `%begin` number of the next block whose flags are 1. Blocks with flags 0 (the attach) belong to no call.
  - `TerminalLink` owns the `tmux -C` process. It kills the process on `close()` and when its output ends, because an orphaned control client blocks the tmux server.
  - `TerminalHub` (`@Observable`, main actor) opens again after a drop with `Backoff` and sets `pause-after=5`. It draws each attached pane from `display-message` (size, cursor, modes) and `capture-pane -p -e -J -S -2000`. It ignores output that the capture already has (it tracks reply numbers in the event stream).
  - A pane that paused while hidden gets `refresh-client -A '%p:continue'` and a redraw when it shows again.
  - Keys go as `send-keys -H` (256 bytes for each command). Sizes go as `refresh-client -C @w:CxR`, deduplicated for each window.
  - If the size of a pane is different from the grid of the view (because the TUI shows it), the pane is letterboxed with a note.
- Terminal views (`AgentwsViews/TerminalPane.swift`, macOS only, SwiftTerm):
  - There is one `PaneHost` for each pane id, cached in `TerminalViews`. Thus a switch of sessions keeps scrollback and cursor.
  - In a focused terminal, each key without ⌘ goes to the pane (`performKeyEquivalent`, which decides by `TerminalKeys.goesToPane`). Thus ⌃C, Esc and ⌃H/J/K/L get to agents and nvim.
  - The exceptions are the combos bound in Settings › Shortcuts (`Shortcuts.bound`). For example, ⌃Space by default goes to the next waiting session. When you bind an action again or clear it, its old combo goes to the pane again.
  - After a session starts from the new-session sheet, `TerminalViews.focusNextShown()` gives keyboard focus to the next pane shown (the pane of the new session), after the sheet closes.
  - `LiveWindow` makes the hub from the `client.native` of the `ViewStore` and gives it to the views as `\.agentwsTerminals`. Without it (snapshots, `--demo`), the main column shows a placeholder.
- Session commands (`SessionCommands.swift`): the stored shortcuts for these commands:
  - Rename and pin (`session.rename`).
  - Model and Effort: choices from `session.options` for the harness of the session, then `session.switch`.
  - Mute (`session.mute`, which flips the flag).
  - End session (`session.end`). It asks for confirmation while "Confirm before ending" in General is on.
  - Resume ended (`session.resume`, only on an ended session).
  - Kill dev servers: `ports.kill` with the process groups of the worktree ports of the session, or "No dev servers…" when there are none.

  The context menu of the sidebar offers the same commands. If a command fails, the main column shows the message of the daemon. Each `ShortcutAction` binds from `Shortcuts.combo(for:)` (Worktrees and disk on the `Window` scene). The diff layout in Appearance is the layout that a new review opens in (`ReviewController(layout:)`). A review that opens again for a different session keeps the current layout. Test: `swift test --filter SessionCommands`.
- Settings (`Settings.swift`, `Shortcuts.swift`, `ServerSettings.swift`, `SettingsScene.swift`): screens 7a–7f. Servers is described below.
  - App-only preferences (`AppSettings`: General, Notifications, Appearance, Shortcuts) are in `UserDefaults`, one JSON blob for each section under `settings.<section>`. Thus if one section stops decoding, only that section goes back to its defaults.
  - `Shortcuts` refuses these combos:
    - a combo that a different action has (`ShortcutRefusal.taken(by:)` names it)
    - ⌘1–⌘9 (session jumps)
    - the fixed window keys in `Shortcuts.fixed` (⌃⌘S, ⇧⌘↩, ⌘,)
    - each combo without ⌘ or ⌃, so a focused terminal keeps plain keys.

    `restoreDefault` skips a default that a different action now has.
  - `CLILink` links `/usr/local/bin/agentws` to the `agentws` of the app. It never replaces or removes a file that it did not make.
  - Workspaces and Agents are for each server. `ServerSettings` calls `workspace.list/add/remove` and `onboarding.status/install/remove/nvim` through `Caller` (the protocol that `ReviewController` uses, and `ViewStore` conforms to). `onboarding.remove` first copies the hook file to a backup.
  - The `config.toml` settings of the server (`ServerConfig.swift`) come from `config.get`. `config.set` saves them one key at a time. It backs up the file and keeps the comments of the user (see [internal/daemon/](../internal/daemon/AGENTS.md#config)). `ConfigFields` lists them for each tab:
    - Workspaces: `launcher.max_parallel`, and the read-only worktree location beside `config.toml` and the cleanup schedule.
    - Agents: the `[defaults.<harness>]` model and effort as pickers from `session.options`, and `fallback.threshold`.
    - Notifications: `push.away_after` and the phones (`device.list`, `device.revoke`, `pair.code`).
    - Appearance: the `[theme]` overrides.
  - With a daemon that does not have `config.get` or `device.list` (`unknown_method`), those groups are empty and show a note.
  - You cannot set the cleanup interval, stale-limit age, worktree location and setup recipes. They are fixed in the daemon, or they are in the `.agentws.toml` of each repo.
- Shell and nvim views (`ShellNvim.swift`):
  - `MainView` is the view switcher (Terminal, Review, Shell, nvim).
  - `Toolbar.views(session:)` gives the tabs. With no selected session, Review, Shell and nvim are grey and have a tooltip (`Toolbar.needsSession`). A click on one, or its shortcut, shows that text as the window message. Thus a click never does nothing.
  - `EmptyMain` is the main column with no session: "Waiting for the daemon", "No sessions yet" or "Select a session", with a New session button.
  - `ShellNvim` (main actor, over `Caller`) calls `shell.focus` for ⌘T and ⇧⌘T. ⌘T is for the selected session: the daemon selects its first worktree, else the root of the session. ⇧⌘T shows the same shell as a popup over the current view.
  - Outside review, ⌘E calls `nvim.toggle`. In review, ⌘E calls `nvim.open` on the top visible line and shows the returned pane in the nvim view.
  - It keeps the shell and nvim pane of each session. Thus the main column draws that pane instead of the pane of the agent.
  - When you press the shortcut again, select Terminal or select a different session, only the view changes back. The shell and nvim continue to run.
  - Test: `swift test --filter 'Shell|Nvim'`.
- Attention (`Attention.swift`, screen 4; views `PermissionCardView`, `AttentionMenuView`, `UserNotifier`):
  - `NoticeStream` keeps a `notify.stream` process (with backoff, as `ViewStore` does) and gives each notice to `Attention`.
  - The `Notifier` port posts a banner (Go field names) as one notification for each session. The session ID is the request and thread identifier, so a newer banner replaces the older one. `remove` withdraws it.
  - `Attention.view(session:front:)` sends `client.viewing` only when the pair changes, and again after a reconnect. While the app is in front, it skips banners for the session in view and withdraws the banner of that session.
  - Settings › Notifications apply in `receive`:
    - `NotificationSettings.alert(for:)` selects the banner and sound toggles by state: permission, waiting, done, and limit or error for all other states.
    - "Skip the session in view" turns that skip off.
    - The app drops a muted session (`isMuted`, read from the view), unless muted sessions notify. The daemon itself never sends a banner for a muted session, so that toggle is important only if it ever does.
  - When these settings hold back a banner, the app withdraws the earlier banner of the session, because the new one would have replaced it.
  - The app keeps a failed `client.viewing` report and sends it again on the next reconnect.
  - `ViewStore.callsRestarted` fires when a call connection replaces one that dropped. The app then sends `client.viewing` again, because the daemon forgets the viewing of a client with its connection.
  - New session… in the menu bar asks the main window (`WindowRouter.newSession()`) to open the new-session sheet.
  - Each `LiveWindow` reports with its own window id. It counts as front while it is the key window. Thus a window that goes to the back never overrides the window that is now in front.
  - Only the latest `refreshCard` applies. Thus a slow reply for the previous session cannot replace the card.
  - The permission card (`refreshCard`) is `session.prompt` while the selected session is in `permission`. It is hidden when the dialog has no parsed choices.
    - Answers go to `session.answer` with the prompt id.
    - A `stale` or `not_found` answer clears the card and the banner, and shows "That prompt is gone; nothing was sent."
    - Allow is the first choice. Always is the next choice that starts with "Yes".
  - `AttentionMenu` supplies the menu bar extra (Needs you, Done · unread, Working, quota meters) and the Dock badge (sessions that need you).
  - `BridgeAgents` finds `~/Library/LaunchAgents/dev.agentws.bridge.*.plist` and builds `agentws setup bridge --remove <host>`. The menu offers it only for a bridge to the server that the app talks to.
  - The app starts the store, notifier and stream from the menu bar label. Thus all of it works when all windows are closed.
  - Without permission, `UserNotifier` works with less function (ad-hoc signed, not notarized, or run outside the bundle). `problem` tells why, and the menu and badge still work.
  - `swift test --filter Attention` runs the suite.
- Servers and first run (`Servers.swift`, `AgentwsViews/FirstRunWindow.swift`, screens 6 and 8):
  - `ServerList` keeps the servers (This Mac first, each SSH host one time) and the selected server in `UserDefaults` (`servers`, `servers.selected`).
  - The toolbar chip is a menu. It switches servers (the app makes a new `ViewStore` for each server), or it opens the "Add a server" window.
  - The "Add a server" window is the first-run flow (`FirstRunFlow`: Welcome, Where agents run, Server, Set up, Workspace, Done). This Mac skips Server.
  - The flow is a Setup Assistant layout that fills its window:
    - a sidebar list of `FirstRunFlow.steps` (ticked by `isComplete`)
    - the content of the step on the window background, with stock controls (radio-group pickers, `GroupBox`es through `SettingsGroup`, rounded text fields)
    - a bottom bar with Cancel (Esc, ⌘.), Back and the default button `primaryTitle`. On first launch, Cancel quits. Else it closes "Add a server".
  - `Snapshot.renderFirstRun` draws each step (`first-run-<n>[-dark].png`).
  - Hosts come from `~/.ssh/config` (`SSHConfig.hosts`, without wildcards) or a typed `user@host`. `SSHConfig.target` refuses options and spaces.
  - `ServerSetup` runs each remote step through `ProcessShell`: `ssh -T -o BatchMode=yes -o ConnectTimeout=10 <host> sh -c '<script>'`. Thus a host that asks for a password fails immediately (exit 255), with a message about key, agent or Tailscale SSH login.
  - The probe prints `uname -s`/`-m`, user, home, the installed build (`~/.local/bin/agentws version --build`) and `setup daemon --check`. It also prints which of git, tmux and gh exist, and if gh is signed in.
  - `setUp` does these steps:
    1. It copies the bundled `Contents/Resources/bin/<os>_<arch>/agentws` through the stdin of the same ssh to a temp file in `~/.local/bin`.
    2. It `mv`s the file into place (atomic). If the build already matches, it skips this, so a second run changes nothing.
    3. It runs `agentws setup daemon`.
    4. If it replaced the binary of a running daemon, it restarts the service (`systemctl --user restart agentws-daemon.service`, or `launchctl kickstart -k` on a Mac). It never stops the service, so tmux sessions continue.
  - `Checklist.items` changes the probe into the rows of the Servers tab, with fix-its: Set up, Update server, the linger command `sudo loginctl enable-linger <user>`, `ssh -t <host> gh auth login`.
  - Hooks and the workspace step use `ServerSettings` over the `ViewStore` of the new server. `ServerSettings.dirs` browses with `workspace.dirs`.
  - `swift test --filter Servers` runs the suite. Its fake `ssh`, `uname` and `systemctl` scripts run the remote side locally against a temp home.
- Tests decode `internal/view/testdata/view-subscribe-*.json` (the Go goldens) and check that each non-null field round-trips. Transport tests put fake `agentws` and `ssh` scripts on `PATH`. The fake `ssh` blocks on `/dev/tty` if it does not get `BatchMode=yes`.

## Why no Xcode project

The app target is a SwiftPM executable and `build-app`, not an `.xcodeproj`. All files are plain text that you can edit and review on Linux. There is no generated project to keep in sync and no XcodeGen step. `swift test` runs the tests of `AgentwsKit` on each machine that has a Swift toolchain. Only the app bundle needs macOS. To use Xcode, open `Package.swift` (File > Open). Xcode reads the package directly.

## Commands

- `swift test` (in `macos/`): the `AgentwsKit` tests. On Linux, install a toolchain with [swiftly](https://www.swift.org/install/linux/) (`swiftly install latest`).
- `swift test --filter AgentwsKitTests.ProtocolTests`: one suite. `swift test --filter Terminal` runs the terminal suites. Their fake `tmux` scripts use control mode.
- `macos/build-app` (macOS only): makes `macos/build/agentws.app`. `open macos/build/agentws.app` runs it. `open macos/build/agentws.app --args --demo` shows the seeded window without a daemon.
- CI's `macos` job runs both on `macos-15`.

## Tests

- Give each test file the name of the one suite type in it (`ProtocolTests.swift` holds `struct ProtocolTests`). For each changed test file, `scripts/tdd-check` runs `swift test --filter <Target>.<File>` on base. A filter that matches nothing exits 0, and that counts as a pass on base.
- For tdd-check, test files count as Go tests do. Commit them before the code. They must fail on base (a compile error counts). A base without `macos/Package.swift` counts as a failure.
- `swift test --filter Review` runs the review suites (`ReviewTests`, `ReviewControllerTests`). The controller tests answer calls from a `FakeCaller`.
- Snapshots: `SnapshotTests` (macOS only, `#if canImport(AppKit)`) renders seeded `WindowScene`s and `SettingsScene`s (one PNG for each settings tab) through `NSHostingView`. It writes PNGs under `$AGENTWS_SNAPSHOTS` (default `$TMPDIR/agentws-snapshots`) and checks that the state colours appear.
  - CI uploads them as the `macos-snapshots` artifact. A UI PR embeds them from the `pr-assets` branch under `<issue>/`.
  - The tests do not compare them to stored references, because a change of the runner image changes the antialiasing.
- `scripts/lint-comments` covers Swift: no `//`, `///` or `/* */` comments. Only the `// swift-tools-version:` line of `Package.swift` passes.
