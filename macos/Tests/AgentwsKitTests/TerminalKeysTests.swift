import Testing
@testable import AgentwsKit

struct TerminalKeysTests {
    let bound = Shortcuts.defaults.bound

    @Test func controlSpaceJumpsToTheNextWaitingSessionInsteadOfReachingThePane() {
        #expect(!TerminalKeys.goesToPane(key: " ", modifiers: [.control], bound: bound))
    }

    @Test func controlLettersEscapeAndSpaceStillReachThePane() {
        for key in ["c", "h", "j", "k", "l"] {
            #expect(TerminalKeys.goesToPane(key: key, modifiers: [.control], bound: bound), "\(key)")
        }
        #expect(TerminalKeys.goesToPane(key: "\u{1b}", modifiers: [], bound: bound))
        #expect(TerminalKeys.goesToPane(key: " ", modifiers: [], bound: bound))
    }

    @Test func commandShortcutsStayWithTheApp() {
        #expect(!TerminalKeys.goesToPane(key: "1", modifiers: [.command], bound: bound))
        #expect(!TerminalKeys.goesToPane(key: " ", modifiers: [.command, .control], bound: bound))
    }

    @Test func aReboundNextWaitingFreesControlSpaceAndTakesItsNewCombo() throws {
        var shortcuts = Shortcuts.defaults
        try shortcuts.assign(KeyCombo("g", [.control]), to: .nextWaiting)
        #expect(TerminalKeys.goesToPane(key: " ", modifiers: [.control], bound: shortcuts.bound))
        #expect(!TerminalKeys.goesToPane(key: "g", modifiers: [.control], bound: shortcuts.bound))
    }

    @Test func anUnboundActionLeavesItsComboToThePane() throws {
        var shortcuts = Shortcuts.defaults
        try shortcuts.assign(nil, to: .nextWaiting)
        #expect(TerminalKeys.goesToPane(key: " ", modifiers: [.control], bound: shortcuts.bound))
    }
}
