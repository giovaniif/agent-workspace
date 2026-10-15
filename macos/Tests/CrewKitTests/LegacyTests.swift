import Foundation
import Testing
@testable import CrewKit

extension FakeServer {
    func installLegacy(build: String) throws {
        let dir = home + "/.local/bin"
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try crew(build: build).write(toFile: dir + "/agentws", atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: dir + "/agentws")
        try bin.write("unit", "")
    }
}

@MainActor
struct LegacyTests {
    @Test(.timeLimit(.minutes(1))) func aFreshServerGetsNoLinkNamedAfterTheOldTool() async throws {
        let server = try FakeServer()
        let setup = server.setup()
        await setup.setUp()
        #expect(setup.error == nil)
        #expect(!FileManager.default.fileExists(atPath: server.home + "/.local/bin/agentws"))
    }

    @Test(.timeLimit(.minutes(1))) func aServerWithOnlyTheOldBinaryGetsCrewAndKeepsTheOldFile() async throws {
        let server = try FakeServer()
        try server.installLegacy(build: "v1.2.0 abc")
        let setup = server.setup()
        await setup.setUp()
        #expect(setup.error == nil)
        #expect(server.remoteBuild == "v1.2.0 abc")
        #expect(FileManager.default.fileExists(atPath: server.home + "/.local/bin/agentws"))
    }

    @Test func aProbeTellsWhenItFoundOnlyTheOldBinary() {
        #expect(Probe.parse("kernel=Linux\nlegacy=1\nbuild=v1\n").legacy)
        #expect(!Probe.parse("kernel=Linux\nbuild=v1\n").legacy)
    }

    @Test func theOldBinaryVariableIsReadAfterTheCrewOne() {
        let old = Build.binary(environment: [LegacyNames.binaryEnvironment: "/opt/old"], resources: "/App/Contents/Resources", arch: "arm64") { _ in true }
        #expect(old == "/opt/old")
        let both = Build.binary(environment: ["CREW_BINARY": "/opt/crew", LegacyNames.binaryEnvironment: "/opt/old"], resources: "/App/Contents/Resources", arch: "arm64") { _ in true }
        #expect(both == "/opt/crew")
    }

    @Test func theOldCLIPathIsWhereTheOldAppLinkedItsBinary() {
        #expect(CLILink.legacyPath == "/usr/local/bin/agentws")
    }

    @Test func aLinkThatTheOldAppMadeNowPointsAtCrew() throws {
        let dir = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("crew-legacy-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let target = dir.appendingPathComponent("crew.app/Contents/Resources/bin/darwin_arm64/crew").path
        let old = dir.appendingPathComponent("old").path
        try FileManager.default.createSymbolicLink(atPath: old, withDestinationPath: "/Applications/agentws.app/Contents/Resources/bin/darwin_arm64/agentws")
        #expect(CLILink.adoptLegacy(path: old, target: target))
        #expect(try FileManager.default.destinationOfSymbolicLink(atPath: old) == target)
        #expect(!CLILink.adoptLegacy(path: old, target: target))

        let mine = dir.appendingPathComponent("mine").path
        try FileManager.default.createSymbolicLink(atPath: mine, withDestinationPath: "/opt/bin/crew")
        #expect(!CLILink.adoptLegacy(path: mine, target: target))
        #expect(!CLILink.adoptLegacy(path: dir.appendingPathComponent("none").path, target: target))

        let other = dir.appendingPathComponent("other").path
        try FileManager.default.createSymbolicLink(atPath: other, withDestinationPath: "/Applications/Other.app/Contents/Resources/bin/darwin_arm64/crew")
        #expect(!CLILink.adoptLegacy(path: other, target: target))
    }

    @Test func crewAndNotYetMovedOldBridgesAreBothFound() throws {
        let bin = try FakeBin()
        for label in ["dev.crew.bridge.new", "dev.agentws.bridge.old"] {
            let plist: [String: Any] = ["Label": label, "ProgramArguments": ["/usr/local/bin/crew", "notify", "bridge", String(label.split(separator: ".").last!)]]
            let data = try PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0)
            try data.write(to: bin.dir.appendingPathComponent(label + ".plist"))
        }
        #expect(BridgeAgents.find(in: bin.dir).map(\.host).sorted() == ["new", "old"])
    }
}
