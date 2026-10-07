import Foundation
import Testing
@testable import AgentwsKit

struct BuildTests {
    @Test func readsTheExactBuildFromTheBinary() throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", """
        printf '%s\\n' "$*" > "\(bin.path("args"))"
        printf 'v0.4.0+abc123\\n'
        """)
        #expect(try Build.read(binary: agentws, environment: bin.environment) == "v0.4.0+abc123")
        #expect(bin.read("args") == "version --build\n")
    }

    @Test func aBinaryThatFailsIsAnError() throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", "exit 3")
        #expect(throws: AgentwsError.self) { try Build.read(binary: agentws, environment: bin.environment) }
    }

    @Test func aBinaryThatPrintsNothingIsAnError() throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", "exit 0")
        #expect(throws: AgentwsError.self) { try Build.read(binary: agentws, environment: bin.environment) }
    }

    @Test func agentwsBinaryInTheEnvironmentWins() {
        let found = Build.binary(environment: ["AGENTWS_BINARY": "/opt/agentws"], resources: "/App/Contents/Resources", arch: "arm64") { _ in true }
        #expect(found == "/opt/agentws")
    }

    @Test func theBundledDarwinBuildForTheArchComesNext() {
        let found = Build.binary(environment: [:], resources: "/App/Contents/Resources", arch: "x86_64") { _ in true }
        #expect(found == "/App/Contents/Resources/bin/darwin_amd64/agentws")
        let arm = Build.binary(environment: ["AGENTWS_BINARY": ""], resources: "/App/Contents/Resources", arch: "arm64") { _ in true }
        #expect(arm == "/App/Contents/Resources/bin/darwin_arm64/agentws")
    }

    @Test func withoutABundledBuildItFallsBackToThePath() {
        #expect(Build.binary(environment: [:], resources: "/App/Contents/Resources", arch: "arm64") { _ in false } == "agentws")
        #expect(Build.binary(environment: [:], resources: nil, arch: "arm64") { _ in true } == "agentws")
    }
}
