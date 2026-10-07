import Foundation
import Testing
@testable import AgentwsKit

struct TerminalPanesTests {
    @Test func nativeClientDecodesTheGoReplyWithOmittedFlags() throws {
        let json = #"""
        {"argv":["tmux","-L","agentws","-C","attach","-t","agentws-native-9-1"],"session":"agentws-native-9-1",
         "panes":[{"pane":"%3","window":"@2","session_id":"s1","cols":120,"rows":40,"mouse_any":true,"mouse_sgr":true,"alternate":true,"cursor_keys":true},
                  {"pane":"%5","window":"@4","cols":80,"rows":24,"cursor_visible":true}]}
        """#
        let native = try JSONDecoder().decode(NativeClient.self, from: Data(json.utf8))
        #expect(native.argv.last == "agentws-native-9-1")
        #expect(native.panes[0] == PaneState(pane: "%3", window: "@2", cols: 120, rows: 40, modes: PaneModes(
            mouseAny: true, mouseSGR: true, alternate: true, cursorVisible: false, cursorKeys: true
        )))
        #expect(native.panes[1].modes == PaneModes(cursorVisible: true))
        #expect(native.panes[1].cursor == nil)
    }

    @Test func aQueryLineGivesSizeCursorAndModes() {
        let state = PaneState(query: "%3 @2 120 40 7 39 0 1 1 1 0 1 0")
        #expect(state == PaneState(pane: "%3", window: "@2", cols: 120, rows: 40, cursor: Cursor(x: 7, y: 39), modes: PaneModes(
            mouseButton: true, mouseStandard: true, mouseSGR: true, cursorVisible: true
        )))
        #expect(PaneState(query: "garbage") == nil)
    }

    @Test func primingDrawsHistoryThenPlacesTheCursorAndRestoresModes() {
        let state = PaneState(pane: "%3", window: "@2", cols: 80, rows: 3, cursor: Cursor(x: 4, y: 1), modes: PaneModes(
            mouseAny: true, mouseSGR: true, cursorVisible: false, cursorKeys: true
        ))
        let text = String(decoding: state.prime(history: ["one", "\u{1b}[31mtwo", "three"]), as: UTF8.self)
        #expect(text == "\u{1b}c" + "one\r\n\u{1b}[31mtwo\r\nthree" + "\u{1b}[0m\u{1b}[2;5H" + "\u{1b}[?1h\u{1b}[?1003h\u{1b}[?1006h\u{1b}[?25l")
    }

    @Test func primingAnAlternateScreenSwitchesBeforeDrawing() {
        let state = PaneState(pane: "%3", window: "@2", cols: 80, rows: 2, cursor: Cursor(x: 0, y: 0), modes: PaneModes(alternate: true, cursorVisible: true))
        let text = String(decoding: state.prime(history: ["a", "b"]), as: UTF8.self)
        #expect(text == "\u{1b}c\u{1b}[?1049h\u{1b}[H" + "a\r\nb" + "\u{1b}[0m\u{1b}[1;1H")
    }

    @Test func aPaneAtTheViewsSizeFillsAndAnotherSizeIsLetterboxedWithANote() {
        #expect(Letterbox(paneCols: 120, paneRows: 40, viewCols: 120, viewRows: 40) == nil)
        let box = Letterbox(paneCols: 100, paneRows: 30, viewCols: 120, viewRows: 40)
        #expect(box?.cols == 100)
        #expect(box?.rows == 30)
        #expect(box?.note == "The TUI is showing this pane at 100×30")
    }

    @Test func layoutUpdatesSizesAndAClosedWindowForgetsItsPanes() {
        var panes = TerminalPanes()
        panes.set(PaneState(pane: "%3", window: "@2", cols: 80, rows: 24))
        panes.set(PaneState(pane: "%5", window: "@4", cols: 80, rows: 24))
        #expect(panes.apply(.layout(window: "@2", panes: [PaneSize(pane: "%3", cols: 100, rows: 30)])) == [])
        #expect(panes["%3"]?.cols == 100)
        #expect(panes["%3"]?.rows == 30)
        #expect(panes.apply(.windowClose(window: "@4")) == ["%5"])
        #expect(panes["%5"] == nil)
    }

    @Test func aPausedPaneResumesWhenShownAndNeedsItsHistoryAgain() {
        var panes = TerminalPanes()
        panes.set(PaneState(pane: "%3", window: "@2", cols: 80, rows: 24))
        panes.show("%3")
        #expect(panes.pause("%3") == .resumeAndRedraw)
        panes.hide("%3")
        #expect(panes.pause("%3") == .stayPaused)
        #expect(panes.show("%3") == .resumeAndRedraw)
        #expect(panes.show("%3") == .none)
    }
}
