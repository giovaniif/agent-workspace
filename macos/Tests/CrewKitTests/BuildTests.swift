import Foundation
import Testing
@testable import CrewKit

struct BuildTests {
    @Test func readsTheExactBuildFromTheBinary() throws {
        let bin = try FakeBin()
        let crew = try bin.script("crew", """
        printf '%s\\n' "$*" > "\(bin.path("args"))"
        printf 'v0.4.0+abc123\\n'
        """)
        #expect(try Build.read(binary: crew, environment: bin.environment) == "v0.4.0+abc123")
        #expect(bin.read("args") == "version --build\n")
    }

    @Test func aBinaryThatFailsIsAnError() throws {
        let bin = try FakeBin()
        let crew = try bin.script("crew", "exit 3")
        #expect(throws: CrewError.self) { try Build.read(binary: crew, environment: bin.environment) }
    }

    @Test func aBinaryThatPrintsNothingIsAnError() throws {
        let bin = try FakeBin()
        let crew = try bin.script("crew", "exit 0")
        #expect(throws: CrewError.self) { try Build.read(binary: crew, environment: bin.environment) }
    }

    @Test func crewBinaryInTheEnvironmentWins() {
        let found = Build.binary(environment: ["CREW_BINARY": "/opt/crew"], resources: "/App/Contents/Resources", arch: "arm64") { _ in true }
        #expect(found == "/opt/crew")
    }

    @Test func theBundledDarwinBuildForTheArchComesNext() {
        let found = Build.binary(environment: [:], resources: "/App/Contents/Resources", arch: "x86_64") { _ in true }
        #expect(found == "/App/Contents/Resources/bin/darwin_amd64/crew")
        let arm = Build.binary(environment: ["CREW_BINARY": ""], resources: "/App/Contents/Resources", arch: "arm64") { _ in true }
        #expect(arm == "/App/Contents/Resources/bin/darwin_arm64/crew")
    }

    @Test func withoutABundledBuildItFallsBackToThePath() {
        #expect(Build.binary(environment: [:], resources: "/App/Contents/Resources", arch: "arm64") { _ in false } == "crew")
        #expect(Build.binary(environment: [:], resources: nil, arch: "arm64") { _ in true } == "crew")
    }
}
