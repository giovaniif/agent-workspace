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
        return URL(fileURLWithPath: env ?? NSTemporaryDirectory() + "agentws-snapshots")
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
        let target = Self.rgb(hex)
        var count = 0
        for y in stride(from: 0, to: rep.pixelsHigh, by: 2) {
            for x in stride(from: 0, to: rep.pixelsWide, by: 2) {
                guard let c = rep.colorAt(x: x, y: y)?.usingColorSpace(.sRGB) else { continue }
                let close = abs(c.redComponent * 255 - target.0) < 14
                    && abs(c.greenComponent * 255 - target.1) < 14
                    && abs(c.blueComponent * 255 - target.2) < 14
                if close { count += 1 }
            }
        }
        return count
    }

    static func rgb(_ hex: String) -> (Double, Double, Double) {
        let v = Int(hex.dropFirst(), radix: 16) ?? 0
        return (Double(v >> 16 & 0xff), Double(v >> 8 & 0xff), Double(v & 0xff))
    }

    @Test func theSeededWindowDrawsEveryStateInLatte() throws {
        let rep = try shoot("window-latte", .seeded())
        for tone in [Tone.red, .peach, .blue, .green, .grey] {
            #expect(pixels(rep, near: Palette.latte.hex(tone)) > 10, "\(tone)")
        }
    }

    @Test func darkModeDrawsInMocha() throws {
        let rep = try shoot("window-mocha", .seeded(), dark: true)
        #expect(pixels(rep, near: Palette.mocha.hex(.base)) > 1_000)
        for tone in [Tone.red, .peach, .blue, .green] {
            #expect(pixels(rep, near: Palette.mocha.hex(tone)) > 10, "\(tone)")
        }
    }

    @Test func theInspectorCanBeHidden() throws {
        _ = try shoot("window-no-inspector", .seeded(inspector: false))
    }

    @Test func noDaemonShowsARetryingBanner() throws {
        let rep = try shoot("unavailable", WindowScene(state: nil, connection: .unavailable("no agentws daemon is running")))
        #expect(pixels(rep, near: Palette.latte.hex(.peach)) > 50)
    }

    @Test func aBuildMismatchShowsAStoppedBanner() throws {
        let rep = try shoot("version-mismatch", WindowScene(
            state: nil,
            connection: .versionMismatch("daemon runs agentws v0.5.0+def but this client is v0.4.0+abc; restart the daemon")
        ))
        #expect(pixels(rep, near: Palette.latte.hex(.red)) > 50)
    }

    @Test func aDroppedConnectionKeepsTheLastStateUnderTheBanner() throws {
        var scene = WindowScene.seeded()
        scene.connection = .reconnecting(in: .seconds(4))
        _ = try shoot("reconnecting", scene)
    }
}
#endif
