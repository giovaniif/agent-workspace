#if canImport(AppKit)
import AppKit
import Foundation
import Testing
import AgentwsKit
import AgentwsViews

@MainActor
struct SnapshotTests {
    static let size = CGSize(width: 1280, height: 800)

    static var directory: URL {
        let env = ProcessInfo.processInfo.environment["AGENTWS_SNAPSHOTS"]
        if let env { return URL(fileURLWithPath: env) }
        return URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("agentws-snapshots")
    }

    func shoot(_ name: String, _ scene: WindowScene, dark: Bool = false) throws -> NSBitmapImageRep {
        try save(name, Snapshot.render(scene, size: Self.size, dark: dark), size: Self.size)
    }

    func save(_ name: String, _ rep: NSBitmapImageRep, size: CGSize) throws -> NSBitmapImageRep {
        let png = try #require(rep.representation(using: .png, properties: [:]))
        try FileManager.default.createDirectory(at: Self.directory, withIntermediateDirectories: true)
        try png.write(to: Self.directory.appendingPathComponent(name + ".png"))
        #expect(rep.pixelsWide >= Int(size.width))
        #expect(rep.pixelsHigh >= Int(size.height))
        return rep
    }

    func pixels(_ rep: NSBitmapImageRep, near hex: String) -> Int {
        guard let image = rep.cgImage, let space = CGColorSpace(name: CGColorSpace.sRGB) else { return 0 }
        let width = image.width
        let height = image.height
        var bytes = [UInt8](repeating: 0, count: width * height * 4)
        let drawn = bytes.withUnsafeMutableBytes { buffer -> Bool in
            guard let context = CGContext(
                data: buffer.baseAddress, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
                space: space, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue
            ) else { return false }
            context.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
            return true
        }
        guard drawn else { return 0 }
        let target = Self.rgb(hex)
        var count = 0
        for i in stride(from: 0, to: bytes.count, by: 4) {
            let close = abs(Double(bytes[i]) - target.0) < 20
                && abs(Double(bytes[i + 1]) - target.1) < 20
                && abs(Double(bytes[i + 2]) - target.2) < 20
            if close { count += 1 }
        }
        return count
    }

    static func rgb(_ hex: String) -> (Double, Double, Double) {
        let v = Int(hex.dropFirst(), radix: 16) ?? 0
        return (Double(v >> 16 & 0xff), Double(v >> 8 & 0xff), Double(v & 0xff))
    }

    func shootDisk(_ name: String, _ scene: DiskScene, dark: Bool = false) throws -> NSBitmapImageRep {
        let rep = Snapshot.render(disk: scene, size: Self.size, dark: dark)
        let png = try #require(rep.representation(using: .png, properties: [:]))
        try FileManager.default.createDirectory(at: Self.directory, withIntermediateDirectories: true)
        try png.write(to: Self.directory.appendingPathComponent(name + ".png"))
        return rep
    }

    @Test func theDiskWindowMarksDirtyAndMergedWorktrees() throws {
        let rep = try shootDisk("disk-worktrees", .seeded())
        #expect(pixels(rep, near: Palette.latte.hex(.peach)) > 20)
        #expect(pixels(rep, near: Palette.latte.hex(.green)) > 20)
    }

    @Test func theDiskWindowShowsMeasuringSizesInMocha() throws {
        _ = try shootDisk("disk-measuring-mocha", .seeded(measuring: true), dark: true)
    }

    @Test func theDiskWindowListsPortsAndRecentCleanups() throws {
        _ = try shootDisk("disk-ports", .seeded(tab: .ports))
        _ = try shootDisk("disk-recent", .seeded(tab: .recent))
    }

    @Test func theSeededWindowDrawsEveryStateInLatte() throws {
        let rep = try shoot("window-latte", .seeded())
        for tone in [Tone.red, .peach, .blue, .green, .grey] {
            #expect(pixels(rep, near: Palette.latte.hex(tone)) > 20, "\(tone)")
        }
    }

    @Test func darkModeDrawsInMocha() throws {
        let rep = try shoot("window-mocha", .seeded(), dark: true)
        #expect(pixels(rep, near: Palette.mocha.hex(.base)) > 1_000)
        for tone in [Tone.red, .peach, .blue, .green] {
            #expect(pixels(rep, near: Palette.mocha.hex(tone)) > 20, "\(tone)")
        }
    }

    @Test func theInspectorCanBeHidden() throws {
        _ = try shoot("window-no-inspector", .seeded(inspector: false))
    }

    @Test func noDaemonShowsARetryingBanner() throws {
        let rep = try shoot("unavailable", WindowScene(state: nil, connection: .unavailable("no agentws daemon is running")))
        #expect(pixels(rep, near: Palette.latte.hex(.peach)) > 100)
    }

    @Test func aBuildMismatchShowsAStoppedBanner() throws {
        let rep = try shoot("version-mismatch", WindowScene(
            state: nil,
            connection: .versionMismatch("daemon runs agentws v0.5.0+def but this client is v0.4.0+abc; restart the daemon")
        ))
        #expect(pixels(rep, near: Palette.latte.hex(.red)) > 100)
    }

    @Test func everySettingsTabRenders() throws {
        for tab in SettingsTab.allCases {
            let size = CGSize(width: 760, height: 560)
            _ = try save("settings-\(tab.rawValue)", Snapshot.settings(.seeded(tab: tab), size: size, dark: false), size: size)
        }
    }

    @Test func theAppearanceTabPreviewsTheTerminalInTheChosenTheme() throws {
        let size = CGSize(width: 760, height: 560)
        var scene = SettingsScene.seeded(tab: .appearance)
        scene.settings.appearance.theme = .mocha
        let rep = try save("settings-appearance-mocha", Snapshot.settings(scene, size: size, dark: false), size: size)
        #expect(pixels(rep, near: Palette.mocha.hex(.base)) > 2_000)
    }

    @Test func aRefusedShortcutShowsWhoHoldsIt() throws {
        let size = CGSize(width: 760, height: 560)
        var scene = SettingsScene.seeded(tab: .shortcuts)
        scene.refusal = "⌘R is already used by Review"
        let rep = try save("settings-shortcut-conflict", Snapshot.settings(scene, size: size, dark: false), size: size)
        #expect(pixels(rep, near: Palette.latte.hex(.red)) > 50)
    }

    @Test func aDroppedConnectionKeepsTheLastStateUnderTheBanner() throws {
        var scene = WindowScene.seeded()
        scene.connection = .reconnecting(in: .seconds(4))
        _ = try shoot("reconnecting", scene)
    }
    @Test func aPermissionSessionShowsTheCardOverItsTerminal() throws {
        let rep = try shoot("permission-card", .seededPermission())
        #expect(pixels(rep, near: Palette.latte.hex(.red)) > 100)
        _ = try shoot("permission-card-mocha", .seededPermission(), dark: true)
    }

    @Test func aStaleAnswerSaysThePromptIsGone() throws {
        var scene = WindowScene.seededPermission()
        scene.card = nil
        scene.message = "That prompt is gone; nothing was sent."
        _ = try shoot("permission-stale", scene)
    }

    @Test func theMenuBarExtraListsWhatNeedsYou() throws {
        let rep = Snapshot.render(menu: .seeded(), size: CGSize(width: 340, height: 620), dark: false)
        let png = try #require(rep.representation(using: .png, properties: [:]))
        try FileManager.default.createDirectory(at: Self.directory, withIntermediateDirectories: true)
        try png.write(to: Self.directory.appendingPathComponent("menu-bar.png"))
        #expect(pixels(rep, near: Palette.latte.hex(.red)) > 20)
    }

    @Test func reviewModeDrawsTheHighlightedDiffAndTheDraft() throws {
        let rep = try shoot("review-latte", .seededReview())
        #expect(pixels(rep, near: Palette.latte.hex(.green)) > 100)
        #expect(pixels(rep, near: Palette.latte.hex(.red)) > 50)
    }

    @Test func reviewModeInMocha() throws {
        let rep = try shoot("review-mocha", .seededReview(), dark: true)
        #expect(pixels(rep, near: Palette.mocha.hex(.base)) > 1_000)
    }

    @Test func reviewModeSplitsTheDiff() throws {
        _ = try shoot("review-split", .seededReview(layout: .split))
    }

    @Test func aRefusedHunkShowsGitsMessage() throws {
        var scene = WindowScene.seededReview()
        scene.review?.error = "error: patch failed: src/session.ts:12"
        let rep = try shoot("review-hunk-refused", scene)
        #expect(pixels(rep, near: Palette.latte.hex(.red)) > 50)
    }

    static let sheet = CGSize(width: 600, height: 640)

    @Test func theNewSessionSheetShowsTheWorkItemCardAndTheQuotaWarning() throws {
        let rep = try save("new-session", Snapshot.renderNewSession(Seed.newSession(), tab: .session, size: Self.sheet, dark: false), size: Self.sheet)
        #expect(pixels(rep, near: Palette.latte.hex(.peach)) > 20)
        #expect(pixels(rep, near: Palette.latte.hex(.blue)) > 20)
    }

    @Test func theNewSessionSheetDrawsInMocha() throws {
        let rep = try save("new-session-mocha", Snapshot.renderNewSession(Seed.newSession(), tab: .session, size: Self.sheet, dark: true), size: Self.sheet)
        #expect(pixels(rep, near: Palette.mocha.hex(.base)) > 1_000)
    }

    @Test func theLauncherTabShowsTheQueueAndTheLimit() throws {
        _ = try save("launcher", Snapshot.renderNewSession(Seed.newSession(), tab: .launcher, size: Self.sheet, dark: false), size: Self.sheet)
    }

    @Test func aSetupFailureIsDrawnInTheSheet() throws {
        let rep = try save("new-session-failed", Snapshot.renderNewSession(Seed.newSession(failure: Seed.setupFailure), tab: .session, size: Self.sheet, dark: false), size: Self.sheet)
        #expect(pixels(rep, near: Palette.latte.hex(.red)) > 20)
    }

    @Test func aServerWithNoWorkspacesOffersToAddOneInTheSheet() throws {
        let rep = try save("new-session-no-workspaces", Snapshot.renderNewSession(Seed.newSession(workspaces: false), tab: .session, size: Self.sheet, dark: false), size: Self.sheet)
        #expect(pixels(rep, near: Palette.latte.hex(.peach)) > 20)
    }

    static let firstRun = CGSize(width: 860, height: 600)

    @Test func theFirstRunWindowFillsTheWindowWithASidebarOfSteps() throws {
        for step in FirstRunStep.allCases {
            var flow = FirstRunFlow()
            flow.kind = .ssh(host: "box")
            flow.step = step
            for dark in [false, true] {
                let name = "first-run-\(step.rawValue)\(dark ? "-dark" : "")"
                _ = try save(name, Snapshot.renderFirstRun(flow, hosts: ["box", "devbox"], size: Self.firstRun, dark: dark), size: Self.firstRun)
            }
        }
    }
}
#endif
