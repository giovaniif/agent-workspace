import Foundation
import Testing
@testable import AgentwsKit

let fakeControlClient = #"""
n=100
printf '%%begin 1 %s 0\n%%end 1 %s 0\n' $n $n
while IFS= read -r line; do
  n=$((n+1))
  printf '%%output %%1 got\\040%s\n' "$n"
  printf '%%begin 1 %s 1\n' $n
  case "$line" in
    bad*) printf 'unknown command\n%%error 1 %s 1\n' $n ;;
    *) printf '%s\n%%end 1 %s 1\n' "$line" $n ;;
  esac
done
"""#

struct TerminalLinkTests {
    @Test(.timeLimit(.minutes(1))) func commandsGetTheirOwnRepliesAndNotificationsStream() async throws {
        let bin = try FakeBin()
        let tmux = try bin.script("tmux", fakeControlClient)
        let link = try TerminalLink(argv: [tmux, "-C", "attach"])
        defer { link.close() }
        let first = try await link.command("display -p one")
        let bad = try await link.command("bad thing")
        let second = try await link.command("display -p two")
        #expect(first == ControlReply(number: 101, ok: true, lines: ["display -p one"]))
        #expect(bad == ControlReply(number: 102, ok: false, lines: ["unknown command"]))
        #expect(second.lines == ["display -p two"])
        var outputs: [ControlEvent] = []
        for await event in link.events {
            if case .output = event { outputs.append(event) }
            if outputs.count == 3 { break }
        }
        #expect(outputs == [
            .output(pane: "%1", bytes: Array("got 101".utf8)),
            .output(pane: "%1", bytes: Array("got 102".utf8)),
            .output(pane: "%1", bytes: Array("got 103".utf8)),
        ])
    }

    @Test(.timeLimit(.minutes(1))) func aDeadControlClientFailsPendingAndLaterCommands() async throws {
        let bin = try FakeBin()
        let tmux = try bin.script("tmux", "read -r line\nexit 0\n")
        let link = try TerminalLink(argv: [tmux])
        defer { link.close() }
        await #expect(throws: AgentwsError.disconnected) { try await link.command("display -p x") }
        await #expect(throws: AgentwsError.disconnected) { try await link.command("display -p y") }
    }

    @Test(.timeLimit(.minutes(1))) func closingKillsTheControlClientEvenIfItIgnoresEndOfInput() async throws {
        let bin = try FakeBin()
        let tmux = try bin.script("tmux", """
        echo $$ > "\(bin.path("pid"))"
        trap '' HUP
        while :; do sleep 0.1; done
        """)
        let link = try TerminalLink(argv: [tmux])
        var pid = ""
        while pid.isEmpty {
            try await Task.sleep(for: .milliseconds(20))
            pid = bin.read("pid").trimmingCharacters(in: .whitespacesAndNewlines)
        }
        link.close()
        for await _ in link.events {}
        var alive = true
        for _ in 0..<100 where alive {
            alive = kill(pid_t(pid)!, 0) == 0
            if alive { try await Task.sleep(for: .milliseconds(20)) }
        }
        #expect(!alive)
    }
}
