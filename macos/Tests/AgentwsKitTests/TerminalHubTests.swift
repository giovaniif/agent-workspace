import Foundation
import Testing
@testable import AgentwsKit

private func fakeTmux(_ bin: FakeBin, exitAfterCapture: Bool) throws -> String {
    try bin.script("tmux", """
    n=0
    while IFS= read -r line; do
      printf '%s\\n' "$line" >> "\(bin.path("commands"))"
      n=$((n+1))
      printf '%%begin 1 %s 1\\n' $n
      case "$line" in
        display-message*) printf '%%3 @2 80 24 5 1 0 0 0 0 0 1 0\\n' ;;
        capture-pane*) printf 'hello\\nworld\\n' ;;
      esac
      printf '%%end 1 %s 1\\n' $n
      case "$line" in
        capture-pane*)
          printf '%%output %%3 abc\\n'
          \(exitAfterCapture ? "exit 0" : ":")
          ;;
      esac
    done
    """)
}

private final class Opens: @unchecked Sendable {
    private let lock = NSLock()
    private var count = 0
    func next() -> Int { lock.withLock { count += 1; return count } }
    var value: Int { lock.withLock { count } }
}

@MainActor
private final class Sink {
    var bytes: [UInt8] = []
    var text: String { String(decoding: bytes, as: UTF8.self) }
}

@MainActor
private func waitUntil(_ condition: @MainActor () -> Bool) async throws {
    while !condition() { try await Task.sleep(for: .milliseconds(10)) }
}

@MainActor
struct TerminalHubTests {
    @Test(.timeLimit(.minutes(1))) func anAttachedPaneGetsItsHistoryThenLiveOutputAndSizesGoToItsWindow() async throws {
        let bin = try FakeBin()
        let tmux = try fakeTmux(bin, exitAfterCapture: false)
        let hub = TerminalHub(endpoint: .local(binary: "agentws"), backoff: Backoff(first: .milliseconds(50), limit: .milliseconds(100))) { _, _ in
            NativeClient(argv: [tmux], session: "agentws-native-1-1", panes: [PaneState(pane: "%3", window: "@2", cols: 80, rows: 24)])
        }
        hub.start()
        defer { hub.stop() }
        let sink = Sink()
        hub.attach(pane: "%3") { sink.bytes += $0 }
        try await waitUntil { sink.text.hasSuffix("abc") }
        #expect(sink.text.hasPrefix("\u{1b}chello\r\nworld"))
        #expect(sink.text.contains("\u{1b}[2;6H"))
        #expect(hub.status == .live)
        hub.resize(pane: "%3", cols: 100, rows: 30)
        hub.send(pane: "%3", bytes: [0x1b])
        try await waitUntil { bin.read("commands").contains("send-keys") }
        let commands = bin.read("commands")
        #expect(commands.contains("refresh-client -f pause-after="))
        #expect(commands.contains("refresh-client -C @2:100x30"))
        #expect(commands.contains("send-keys -H -t %3 1b"))
    }

    @Test(.timeLimit(.minutes(1))) func aDroppedLinkReconnectsAndRedrawsEveryAttachedPane() async throws {
        let bin = try FakeBin()
        let tmux = try fakeTmux(bin, exitAfterCapture: true)
        let opens = Opens()
        let hub = TerminalHub(endpoint: .local(binary: "agentws"), backoff: Backoff(first: .milliseconds(50), limit: .milliseconds(100))) { _, _ in
            _ = opens.next()
            return NativeClient(argv: [tmux], session: "s", panes: [])
        }
        hub.start()
        defer { hub.stop() }
        let sink = Sink()
        hub.attach(pane: "%3") { sink.bytes += $0 }
        try await waitUntil { opens.value >= 3 && sink.text.components(separatedBy: "\u{1b}chello").count >= 3 }
    }

    @Test(.timeLimit(.minutes(1))) func aFailedOpenShowsReconnectingAndRetries() async throws {
        let opens = Opens()
        let hub = TerminalHub(endpoint: .local(binary: "agentws"), backoff: Backoff(first: .milliseconds(50), limit: .milliseconds(100))) { _, _ in
            _ = opens.next()
            throw AgentwsError.disconnected
        }
        hub.start()
        defer { hub.stop() }
        try await waitUntil { opens.value >= 2 }
        if case .reconnecting = hub.status {} else { Issue.record("status \(hub.status)") }
    }
}
