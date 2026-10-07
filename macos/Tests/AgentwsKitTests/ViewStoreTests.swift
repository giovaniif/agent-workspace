import Foundation
import Synchronization
import Testing
@testable import AgentwsKit

final class SleepLog: Sendable {
    private let delays = Mutex<[Duration]>([])

    var recorded: [Duration] { delays.withLock { $0 } }

    var sleep: @Sendable (Duration) async throws -> Void {
        { delay in
            self.delays.withLock { $0.append(delay) }
            try await Task.sleep(for: .milliseconds(10))
        }
    }
}

@MainActor
func eventually(_ condition: @MainActor () -> Bool) async {
    for _ in 0..<500 {
        if condition() { return }
        try? await Task.sleep(for: .milliseconds(20))
    }
    Issue.record("condition never became true")
}

func stateLine(seq: Int) throws -> String {
    let raw = try Goldens.data("view-subscribe-state.json")
    var object = try #require(try JSONSerialization.jsonObject(with: raw) as? [String: Any])
    object["seq"] = seq
    let state = try JSONSerialization.data(withJSONObject: object)
    return #"{"v":1,"id":1,"result":"# + String(decoding: state, as: UTF8.self) + "}"
}

func diffLines() throws -> String {
    let raw = try Goldens.data("view-subscribe-diffs.json")
    let diffs = try #require(try JSONSerialization.jsonObject(with: raw) as? [Any])
    return try diffs.map { diff in
        let data = try JSONSerialization.data(withJSONObject: diff)
        return #"{"v":1,"id":1,"diff":"# + String(decoding: data, as: UTF8.self) + "}"
    }.joined(separator: "\n")
}

@MainActor
struct ViewStoreTests {
    @Test(.timeLimit(.minutes(1))) func subscribesAndAppliesTheStream() async throws {
        let bin = try FakeBin()
        try bin.write("stream", try stateLine(seq: 10) + "\n" + diffLines() + "\n")
        let agentws = try bin.script("agentws", """
        IFS= read -r line
        printf '%s' "$line" > "\(bin.path("request"))"
        cat "\(bin.path("stream"))"
        cat > /dev/null
        """)
        let store = ViewStore(endpoint: .local(binary: agentws), build: "v1", environment: bin.environment)
        store.start()
        await eventually { store.state?.seq == 16 }
        #expect(store.connection == .live)
        #expect(store.state?.sessions.map(\.id) == ["s1", "s2"])
        let request = try #require(try JSONSerialization.jsonObject(with: Data(bin.read("request").utf8)) as? [String: Any])
        #expect(request["method"] as? String == "view.subscribe")
        #expect(request["build"] as? String == "v1")
        store.stop()
    }

    @Test(.timeLimit(.minutes(1))) func aKilledTransportReconnectsAndResubscribes() async throws {
        let bin = try FakeBin()
        try bin.write("first", try stateLine(seq: 10) + "\n")
        try bin.write("second", try stateLine(seq: 99) + "\n")
        let agentws = try bin.script("agentws", """
        IFS= read -r line
        echo run >> "\(bin.path("runs"))"
        if [ "$(wc -l < "\(bin.path("runs"))")" -eq 1 ]; then
          cat "\(bin.path("first"))"
          sleep 0.2
          kill -9 $$
        fi
        cat "\(bin.path("second"))"
        cat > /dev/null
        """)
        let log = SleepLog()
        let store = ViewStore(endpoint: .local(binary: agentws), build: "v1", environment: bin.environment, sleep: log.sleep)
        store.start()
        await eventually { store.state?.seq == 99 }
        #expect(log.recorded == [.seconds(1)])
        #expect(bin.read("runs") == "run\nrun\n")
        #expect(store.connection == .live)
        store.stop()
    }

    @Test(.timeLimit(.minutes(1))) func failedConnectsBackOffByDoubling() async throws {
        let bin = try FakeBin()
        try bin.write("state", try stateLine(seq: 10) + "\n")
        let agentws = try bin.script("agentws", """
        echo run >> "\(bin.path("runs"))"
        if [ "$(wc -l < "\(bin.path("runs"))")" -le 4 ]; then exit 1; fi
        IFS= read -r line
        cat "\(bin.path("state"))"
        cat > /dev/null
        """)
        let log = SleepLog()
        let store = ViewStore(endpoint: .local(binary: agentws), build: "v1", environment: bin.environment, sleep: log.sleep)
        store.start()
        await eventually { store.state?.seq == 10 }
        #expect(log.recorded == [.seconds(1), .seconds(2), .seconds(4), .seconds(8)])
        store.stop()
    }

    @Test(.timeLimit(.minutes(1))) func noDaemonSurfacesAsUnavailableAndKeepsRetrying() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", """
        echo run >> "\(bin.path("runs"))"
        printf '{"v":1,"id":0,"error":{"code":"unavailable","message":"no agentws daemon is running"}}\\n'
        exit 1
        """)
        let log = SleepLog()
        let store = ViewStore(endpoint: .local(binary: agentws), build: "v1", environment: bin.environment, sleep: log.sleep)
        store.start()
        await eventually { log.recorded.count >= 2 }
        #expect(store.connection == .unavailable("no agentws daemon is running"))
        store.stop()
    }

    @Test(.timeLimit(.minutes(1))) func aBuildMismatchStopsRetrying() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", """
        echo run >> "\(bin.path("runs"))"
        IFS= read -r line
        printf '{"v":1,"id":1,"error":{"code":"version_mismatch","message":"restart the daemon"}}\\n'
        cat > /dev/null
        """)
        let log = SleepLog()
        let store = ViewStore(endpoint: .local(binary: agentws), build: "v1", environment: bin.environment, sleep: log.sleep)
        store.start()
        await eventually { store.connection == .versionMismatch("restart the daemon") }
        try await Task.sleep(for: .milliseconds(300))
        #expect(log.recorded.isEmpty)
        #expect(bin.read("runs") == "run\n")
        store.stop()
    }

    @Test(.timeLimit(.minutes(1))) func callsUseTheirOwnConnection() async throws {
        let bin = try FakeBin()
        try bin.write("state", try stateLine(seq: 10) + "\n")
        let agentws = try bin.script("agentws", """
        echo run >> "\(bin.path("runs"))"
        IFS= read -r line
        case "$line" in
          *view.subscribe*) cat "\(bin.path("state"))"; cat > /dev/null ;;
        esac
        printf '{"v":1,"id":1,"result":{"echo":1}}\\n'
        \(echoReplies)
        """)
        let store = ViewStore(endpoint: .local(binary: agentws), build: "v1", environment: bin.environment)
        store.start()
        await eventually { store.state?.seq == 10 }
        let reply: Echo = try await store.call("status", params: [String: String]())
        #expect(reply == Echo(echo: 1))
        #expect(bin.read("runs") == "run\nrun\n")
        store.stop()
    }

    @Test(.timeLimit(.minutes(1))) func aNewCallConnectionAfterADropReportsARestart() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", """
        IFS= read -r line
        printf '{"v":1,"id":1,"result":{"echo":1}}\\n'
        """)
        let store = ViewStore(endpoint: .local(binary: agentws), build: "v1", environment: bin.environment)
        var restarts = 0
        store.callsRestarted = { restarts += 1 }
        let _: Echo = try await store.call("status", params: [String: String]())
        #expect(restarts == 0)
        try await Task.sleep(for: .milliseconds(300))
        let _: Echo = try await store.call("status", params: [String: String]())
        #expect(restarts == 1)
        store.stop()
    }
}
