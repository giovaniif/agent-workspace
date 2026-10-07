import Foundation
import Testing
@testable import AgentwsKit

struct TerminalControlTests {
    private func feed(_ lines: [String]) -> [ControlEvent] {
        var parser = ControlParser()
        return lines.compactMap { parser.feed(Array($0.utf8)) }
    }

    @Test func outputUnescapesOctalAndBackslashes() {
        let events = feed([#"%output %3 a\015\012\033[1mb\\c"#])
        #expect(events == [.output(pane: "%3", bytes: Array("a\r\n\u{1b}[1mb\\c".utf8))])
    }

    @Test func outputKeepsUTF8BytesAsTheyCome() {
        let events = feed(["%output %1 é界"])
        #expect(events == [.output(pane: "%1", bytes: Array("é界".utf8))])
    }

    @Test func extendedOutputSkipsTheAgeAndReservedFields() {
        let events = feed([#"%extended-output %7 1532 : hi\040there"#])
        #expect(events == [.output(pane: "%7", bytes: Array("hi there".utf8))])
    }

    @Test func aReplyCarriesItsBeginNumberWhetherItIsOursAndItsBody() {
        let events = feed([
            "%begin 1791342349 332 0", "%end 1791342349 332 0",
            "%begin 1791342349 339 1", "hi", "%output %1 not-a-notification", "%end 1791342349 339 1",
            "%begin 1791342349 342 1", "parse error: unknown command: badcmd", "%error 1791342349 342 1",
        ])
        #expect(events == [
            .begin(number: 332, mine: false),
            .reply(number: 332, ok: true, lines: []),
            .begin(number: 339, mine: true),
            .reply(number: 339, ok: true, lines: ["hi", "%output %1 not-a-notification"]),
            .begin(number: 342, mine: true),
            .reply(number: 342, ok: false, lines: ["parse error: unknown command: badcmd"]),
        ])
    }

    @Test func windowAndFlowNotifications() {
        let events = feed([
            "%window-add @4", "%unlinked-window-add @5", "%window-close @4", "%unlinked-window-close @5",
            "%pause %2", "%continue %2", "%exit", "%exit detached", "%session-changed $0 x",
        ])
        #expect(events == [
            .windowAdd(window: "@4"), .windowAdd(window: "@5"), .windowClose(window: "@4"), .windowClose(window: "@5"),
            .pause(pane: "%2"), .resume(pane: "%2"), .exit(reason: ""), .exit(reason: "detached"),
        ])
    }

    @Test func layoutChangeListsEveryLeafPaneWithItsSize() {
        let events = feed(["%layout-change @2 8c1a,200x50,0,0{30x50,0,0,4,169x50,31,0[169x25,31,0,7,169x24,31,26,9]} 8c1a,200x50,0,0 *"])
        #expect(events == [.layout(window: "@2", panes: [
            PaneSize(pane: "%4", cols: 30, rows: 50),
            PaneSize(pane: "%7", cols: 169, rows: 25),
            PaneSize(pane: "%9", cols: 169, rows: 24),
        ])])
    }

    @Test func layoutOfASinglePaneWindow() {
        let events = feed(["%layout-change @1 b25d,80x24,0,0,3 b25d,80x24,0,0,3 *"])
        #expect(events == [.layout(window: "@1", panes: [PaneSize(pane: "%3", cols: 80, rows: 24)])])
    }

    @Test(.timeLimit(.minutes(1))) func theDecoderKeepsUpWithBusyPanes() {
        let chunk = String(repeating: #"\033[38;5;208mbusy pane output \342\224\200\015\012"#, count: 40)
        let line = Array(("%output %1 " + chunk).utf8)
        var parser = ControlParser()
        let clock = ContinuousClock()
        var bytes = 0
        let elapsed = clock.measure {
            for _ in 0..<2000 {
                if case let .output(_, out) = parser.feed(line) { bytes += out.count }
            }
        }
        #expect(bytes > 2_000_000)
        #expect(elapsed < .seconds(2))
    }

    @Test func repliesMatchCallsByTheirBeginNumberAndIgnoreOtherClientsBlocks() {
        var matcher = ReplyMatcher<String>()
        matcher.sent("first")
        matcher.sent("second")
        #expect(matcher.receive(.begin(number: 10, mine: false)) == nil)
        #expect(matcher.receive(.reply(number: 10, ok: true, lines: [])) == nil)
        #expect(matcher.receive(.begin(number: 11, mine: true)) == nil)
        #expect(matcher.receive(.begin(number: 12, mine: true)) == nil)
        let second = matcher.receive(.reply(number: 12, ok: false, lines: ["no"]))
        #expect(second?.call == "second")
        #expect(second?.ok == false)
        let first = matcher.receive(.reply(number: 11, ok: true, lines: ["yes"]))
        #expect(first?.call == "first")
        #expect(first?.lines == ["yes"])
        #expect(matcher.drain().isEmpty)
    }

    @Test func drainHandsBackEveryUnansweredCall() {
        var matcher = ReplyMatcher<Int>()
        matcher.sent(1)
        matcher.sent(2)
        _ = matcher.receive(.begin(number: 5, mine: true))
        #expect(matcher.drain().sorted() == [1, 2])
    }

    @Test func keysGoAsHexInBoundedChunks() {
        #expect(ControlCommand.sendKeys(pane: "%3", bytes: [0x1b, 0x03, 0x08, 0x61]) == ["send-keys -H -t %3 1b 03 08 61"])
        let chunks = ControlCommand.sendKeys(pane: "%3", bytes: Array(repeating: 0x61, count: 600))
        #expect(chunks.count == 3)
        #expect(chunks.allSatisfy { $0.hasPrefix("send-keys -H -t %3 61") })
        #expect(ControlCommand.sendKeys(pane: "%3", bytes: []).isEmpty)
    }

    @Test func commandsForSizesFlowAndHistory() {
        #expect(ControlCommand.size(window: "@2", cols: 120, rows: 40) == "refresh-client -C @2:120x40")
        #expect(ControlCommand.pauseAfter(seconds: 5) == "refresh-client -f pause-after=5")
        #expect(ControlCommand.resume(pane: "%3") == "refresh-client -A '%3:continue'")
        #expect(ControlCommand.capture(pane: "%3") == "capture-pane -p -e -J -S -2000 -t %3")
        #expect(ControlCommand.query(pane: "%3").hasPrefix("display-message -p -t %3 '#{pane_id} #{window_id} #{pane_width} #{pane_height}"))
    }

    @Test func overSSHTheControlClientRunsWithTheSameOptionsAndQuotedArgv() {
        let argv = ["tmux", "-L", "agentws", "-C", "attach", "-t", "agentws-native-1-2"]
        #expect(Endpoint.local(binary: "agentws").terminalArgv(argv) == argv)
        #expect(Endpoint.ssh(host: "box", remoteBinary: "agentws").terminalArgv(argv) == [
            "ssh", "-T", "-o", "BatchMode=yes", "-o", "RemoteCommand=none", "-o", "RequestTTY=no", "-o", "ServerAliveInterval=15", "box", "--",
            "'tmux' '-L' 'agentws' '-C' 'attach' '-t' 'agentws-native-1-2'",
        ])
        #expect(Endpoint.ssh(host: "box", remoteBinary: "agentws").terminalArgv(["it's"]).last == #"'it'\''s'"#)
    }
}
