import Foundation
import Testing
@testable import AgentwsKit

@MainActor
struct SettingsTests {
    func freshDefaults() -> (UserDefaults, String) {
        let suite = "agentws-tests-\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        return (defaults, suite)
    }

    @Test func everyControlSurvivesARelaunch() throws {
        let (defaults, suite) = freshDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let first = SettingsStore(defaults: defaults)
        first.settings.general = General(
            openAtLogin: true, launchServer: "devbox", menuBarCount: .needsYouAndDone, dockBadge: .none,
            endedShown: 25, confirmOnEnd: false, showWorktrees: false, showSubagents: true, checkUpdates: false
        )
        first.settings.notifications = NotificationSettings(
            permission: EventAlert(banner: true, sound: false),
            waiting: EventAlert(banner: false, sound: true),
            done: EventAlert(banner: false, sound: false),
            limitOrError: EventAlert(banner: true, sound: true),
            skipSessionInView: false,
            notifyMuted: true
        )
        first.settings.appearance = Appearance(
            theme: .mocha, accent: .peach, terminalFont: "JetBrains Mono", fontSize: 14, lineHeight: 1.4,
            cursor: .bar, cursorBlinks: false, density: .compact, diffLayout: .split
        )
        try first.settings.shortcuts.assign(KeyCombo("y", [.command, .option]), to: .inspector)
        first.settings.shortcuts.passThrough = false

        let relaunched = SettingsStore(defaults: UserDefaults(suiteName: suite)!)
        #expect(relaunched.settings == first.settings)
        #expect(relaunched.settings.shortcuts.combo(for: .inspector) == KeyCombo("y", [.command, .option]))
        #expect(relaunched.settings.shortcuts.passThrough == false)
    }

    @Test func aFreshInstallStartsFromTheDesignDefaults() {
        let (defaults, suite) = freshDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let settings = SettingsStore(defaults: defaults).settings
        #expect(settings.appearance.theme == .system)
        #expect(settings.general.launchServer == "This Mac")
        #expect(settings.general.confirmOnEnd)
        #expect(settings.notifications.skipSessionInView)
        #expect(settings.shortcuts.passThrough)
        #expect(settings.shortcuts == Shortcuts.defaults)
    }

    @Test func aSectionThatNoLongerDecodesFallsBackAloneToItsDefaults() throws {
        let (defaults, suite) = freshDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let first = SettingsStore(defaults: defaults)
        first.settings.appearance.theme = .latte
        defaults.set(Data("{\"openAtLogin\":\"yes\"}".utf8), forKey: SettingsStore.Key.general)

        let relaunched = SettingsStore(defaults: defaults)
        #expect(relaunched.settings.general == General())
        #expect(relaunched.settings.appearance.theme == .latte)
    }

    @Test func terminalFontSizeAndLineHeightStayInRange() {
        var appearance = Appearance()
        appearance.fontSize = 60
        appearance.lineHeight = 0.2
        #expect(appearance.fontSize == Appearance.fontSizes.upperBound)
        #expect(appearance.lineHeight == Appearance.lineHeights.lowerBound)
        appearance.fontSize = 2
        appearance.lineHeight = 9
        #expect(appearance.fontSize == Appearance.fontSizes.lowerBound)
        #expect(appearance.lineHeight == Appearance.lineHeights.upperBound)
    }

    @Test func theThemeChoiceResolvesAgainstTheSystemAppearance() {
        #expect(ThemeChoice.system.palette(systemDark: true) == .mocha)
        #expect(ThemeChoice.system.palette(systemDark: false) == .latte)
        #expect(ThemeChoice.latte.palette(systemDark: true) == .latte)
        #expect(ThemeChoice.mocha.palette(systemDark: false) == .mocha)
    }

    @Test func theCLILinkPointsPATHAtTheAppsAgentwsAndNeverReplacesAnotherFile() throws {
        let dir = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("agentws-cli-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let target = dir.appendingPathComponent("bundled-agentws").path
        try Data("#!/bin/sh\n".utf8).write(to: URL(fileURLWithPath: target))
        let link = CLILink(path: dir.appendingPathComponent("bin/agentws").path, target: target)

        #expect(link.status == .missing)
        try link.install()
        #expect(link.status == .linked)
        try link.install()
        #expect(link.status == .linked)
        try link.remove()
        #expect(link.status == .missing)

        let other = dir.appendingPathComponent("other").path
        try FileManager.default.createSymbolicLink(atPath: link.path, withDestinationPath: other)
        #expect(link.status == .elsewhere(other))
        #expect(throws: CLILinkError.self) { try link.install() }
        #expect(throws: CLILinkError.self) { try link.remove() }
        #expect(link.status == .elsewhere(other))
    }
}
