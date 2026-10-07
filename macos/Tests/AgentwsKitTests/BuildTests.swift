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
}
