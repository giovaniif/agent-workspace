import Foundation
import Testing
@testable import AgentwsKit

struct ShortcutSettingsTests {
    @Test func theDefaultsFollowTheKeyboardMapWithTheTUIKeyBeside() {
        let shortcuts = Shortcuts.defaults
        #expect(shortcuts.combo(for: .review) == KeyCombo("r", [.command]))
        #expect(shortcuts.combo(for: .linearLauncher) == KeyCombo("n", [.command, .shift]))
        #expect(shortcuts.combo(for: .nextWaiting) == KeyCombo("space", [.control]))
        #expect(ShortcutAction.review.tuiKey == "r")
        #expect(ShortcutAction.inspector.tuiKey == nil)
        #expect(KeyCombo("n", [.command, .shift]).display == "⇧⌘N")
        #expect(KeyCombo("m", [.control, .option, .shift, .command]).display == "⌃⌥⇧⌘M")
        #expect(KeyCombo("space", [.control]).display == "⌃Space")
        #expect(KeyCombo("delete", [.command]).display == "⌘⌫")
    }

    @Test func noTwoDefaultActionsShareAKey() {
        let combos = ShortcutAction.allCases.compactMap { Shortcuts.defaults.combo(for: $0) }
        #expect(Set(combos).count == combos.count)
    }

    @Test func aConflictIsRefusedWithTheActionThatHoldsIt() {
        var shortcuts = Shortcuts.defaults
        #expect(throws: ShortcutRefusal.taken(by: "Review")) {
            try shortcuts.assign(KeyCombo("r", [.command]), to: .inspector)
        }
        #expect(shortcuts == Shortcuts.defaults)
    }

    @Test func theRefusalMessageNamesTheKeyAndTheHolder() {
        let combo = KeyCombo("r", [.command])
        #expect(ShortcutRefusal.taken(by: "Review").message(for: combo) == "⌘R is already used by Review")
        #expect(ShortcutRefusal.needsModifier.message(for: KeyCombo("r", [])) == "R needs ⌘ or ⌃, so the terminal keeps plain keys")
    }

    @Test func sessionJumpsKeepCommandOneToNine() {
        var shortcuts = Shortcuts.defaults
        #expect(throws: ShortcutRefusal.taken(by: "Jump to session N")) {
            try shortcuts.assign(KeyCombo("4", [.command]), to: .review)
        }
    }

    @Test func aShortcutNeedsCommandOrControlSoTheTerminalKeepsPlainKeys() {
        var shortcuts = Shortcuts.defaults
        #expect(throws: ShortcutRefusal.needsModifier) {
            try shortcuts.assign(KeyCombo("r", [.shift]), to: .review)
        }
        #expect(throws: ShortcutRefusal.needsModifier) {
            try shortcuts.assign(KeyCombo("r", []), to: .review)
        }
    }

    @Test func reassigningAnActionToItsOwnKeyOrClearingItIsAllowed() throws {
        var shortcuts = Shortcuts.defaults
        try shortcuts.assign(KeyCombo("r", [.command]), to: .review)
        #expect(shortcuts == Shortcuts.defaults)
        try shortcuts.assign(nil, to: .review)
        #expect(shortcuts.combo(for: .review) == nil)
        try shortcuts.assign(KeyCombo("r", [.command]), to: .inspector)
        #expect(shortcuts.holder(of: KeyCombo("r", [.command])) == .inspector)
    }

    @Test func restoreDefaultsBringsBackEveryKeyButKeepsPassThrough() throws {
        var shortcuts = Shortcuts.defaults
        shortcuts.passThrough = false
        try shortcuts.assign(nil, to: .review)
        try shortcuts.assign(KeyCombo("r", [.command, .option]), to: .newSession)
        shortcuts.restoreDefault(.review)
        #expect(shortcuts.combo(for: .review) == KeyCombo("r", [.command]))
        #expect(shortcuts.combo(for: .newSession) == KeyCombo("r", [.command, .option]))
        shortcuts.restoreDefaults()
        #expect(shortcuts.combo(for: .newSession) == KeyCombo("n", [.command]))
        #expect(shortcuts.passThrough == false)
    }

    @Test func restoringOneDefaultThatIsNowTakenIsRefused() throws {
        var shortcuts = Shortcuts.defaults
        try shortcuts.assign(nil, to: .review)
        try shortcuts.assign(KeyCombo("r", [.command]), to: .inspector)
        shortcuts.restoreDefault(.review)
        #expect(shortcuts.combo(for: .review) == nil)
        #expect(shortcuts.combo(for: .inspector) == KeyCombo("r", [.command]))
    }

    @Test func shortcutsEncodeByActionName() throws {
        let data = try JSONEncoder().encode(Shortcuts.defaults)
        let text = String(decoding: data, as: UTF8.self)
        #expect(text.contains("\"review\""))
        #expect(try JSONDecoder().decode(Shortcuts.self, from: data) == Shortcuts.defaults)
    }
}
