import Foundation
import Testing
import AgentwsKit

@MainActor
struct LiveSidebarTests {
    @Test(.timeLimit(.minutes(1))) func aHookReachesTheSidebarWithin150msAtTheMedianOfSeveralDiffs() async throws {
        let bin = try FakeBin()
        let state = Seed.state(sessions: [SeedSession(id: "s1", name: "a", state: "running")])
        let encoder = JSONEncoder()
        try bin.write("state", #"{"v":1,"id":1,"result":"# + String(decoding: try encoder.encode(state), as: UTF8.self) + "}\n")
        let samples = 7
        for index in 1...samples {
            var session = state.sessions[0]
            session.state = index % 2 == 1 ? "permission" : "running"
            var diff = ViewDiff(seq: state.seq + index)
            diff.session = session
            try bin.write("diff\(index)", #"{"v":1,"id":1,"diff":"# + String(decoding: try encoder.encode(diff), as: UTF8.self) + "}\n")
        }
        let agentws = try bin.script("agentws", """
        IFS= read -r line
        cat "\(bin.path("state"))"
        i=1
        while [ $i -le \(samples) ]; do
          while [ ! -f "\(bin.path("go"))$i" ]; do sleep 0.005; done
          cat "\(bin.path("diff"))$i"
          i=$((i + 1))
        done
        cat > /dev/null
        """)
        let store = ViewStore(endpoint: .local(binary: agentws), build: "v1", environment: bin.environment)
        store.start()
        defer { store.stop() }
        await eventually { store.connection == .live }
        let glyph = { store.state.map { Sidebar(state: $0).rows.first?.style.glyph } ?? nil }
        #expect(glyph() == .spinningRing)
        let clock = ContinuousClock()
        var latencies: [Duration] = []
        for index in 1...samples {
            let expected: Glyph = index % 2 == 1 ? .filledDiamond : .spinningRing
            let start = clock.now
            try bin.write("go\(index)", "")
            while clock.now - start < .seconds(2), glyph() != expected {
                try await Task.sleep(for: .milliseconds(2))
            }
            #expect(glyph() == expected)
            latencies.append(clock.now - start)
        }
        let median = latencies.sorted()[samples / 2]
        #expect(median < .milliseconds(150), "latencies: \(latencies)")
    }
}
