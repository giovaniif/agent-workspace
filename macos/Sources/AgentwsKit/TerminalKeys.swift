public enum TerminalKeys {
    public static func goesToPane(key: String, modifiers: Modifiers, bound: Set<KeyCombo>) -> Bool {
        if modifiers.contains(.command) { return false }
        return !bound.contains(KeyCombo(name(of: key), modifiers))
    }

    static func name(of key: String) -> String {
        switch key {
        case " ": "space"
        case "\r": "return"
        case "\u{1b}": "escape"
        case "\t": "tab"
        case "\u{7f}", "\u{8}": "delete"
        default: key
        }
    }
}
