import Testing
@testable import AgentwsKit

struct TerminalKeysTests {
    @Test func controlSpaceJumpsToTheNextWaitingSessionInsteadOfReachingThePane() {
        #expect(!TerminalKeys.goesToPane(key: " ", command: false, control: true))
    }

    @Test func controlLettersEscapeAndSpaceStillReachThePane() {
        for key in ["c", "h", "j", "k", "l"] {
            #expect(TerminalKeys.goesToPane(key: key, command: false, control: true), "\(key)")
        }
        #expect(TerminalKeys.goesToPane(key: "\u{1b}", command: false, control: false))
        #expect(TerminalKeys.goesToPane(key: " ", command: false, control: false))
    }

    @Test func commandShortcutsStayWithTheApp() {
        #expect(!TerminalKeys.goesToPane(key: "1", command: true, control: false))
        #expect(!TerminalKeys.goesToPane(key: " ", command: true, control: true))
    }
}
