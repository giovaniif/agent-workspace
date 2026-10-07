import Foundation
import Testing
import AgentwsKit

@MainActor
struct LiveSidebarTests {
    @Test(.timeLimit(.minutes(1))) func aHookReachesTheSidebarWithin150ms() async throws {
        let bin = try FakeBin()
        let state = Seed.state(sessions: [SeedSession(id: "s1", name: "a", state: "running")])
        var asking = state.sessions[0]
        asking.state = "permission"
        var diff = ViewDiff(seq: state.seq + 1)
        diff.session = asking
        let encoder = JSONEncoder()
        try bin.write("state", #"{"v":1,"id":1,"result":"# + String(decoding: try encoder.encode(state), as: UTF8.self) + "}\n")
        try bin.write("diff", #"{"v":1,"id":1,"diff":"# + String(decoding: try encoder.encode(diff), as: UTF8.self) + "}\n")
        let agentws = try bin.script("agentws", """
        IFS= read -r line
        cat "\(bin.path("state"))"
        while [ ! -f "\(bin.path("go"))" ]; do sleep 0.005; done
        cat "\(bin.path("diff"))"
        cat > /dev/null
        """)
        let store = ViewStore(endpoint: .local(binary: agentws), build: "v1", environment: bin.environment)
        store.start()
        defer { store.stop() }
        await eventually { store.connection == .live }
        let glyph = { store.state.map { Sidebar(state: $0).rows.first?.style.glyph } ?? nil }
        #expect(glyph() == .spinningRing)
        let clock = ContinuousClock()
        let start = clock.now
        try bin.write("go", "")
        while clock.now - start < .seconds(2), glyph() != .filledDiamond {
            try await Task.sleep(for: .milliseconds(2))
        }
        #expect(glyph() == .filledDiamond)
        #expect(clock.now - start < .milliseconds(150))
    }
}
