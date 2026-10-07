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
        let rep = Snapshot.render(scene, size: Self.size, dark: dark)
        let png = try #require(rep.representation(using: .png, properties: [:]))
        try FileManager.default.createDirectory(at: Self.directory, withIntermediateDirectories: true)
        try png.write(to: Self.directory.appendingPathComponent(name + ".png"))
        #expect(rep.pixelsWide >= Int(Self.size.width))
        #expect(rep.pixelsHigh >= Int(Self.size.height))
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

    @Test func aDroppedConnectionKeepsTheLastStateUnderTheBanner() throws {
        var scene = WindowScene.seeded()
        scene.connection = .reconnecting(in: .seconds(4))
        _ = try shoot("reconnecting", scene)
    }
}
#endif
