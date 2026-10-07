import Foundation

public struct Modifiers: OptionSet, Codable, Hashable, Sendable {
    public let rawValue: Int

    public init(rawValue: Int) { self.rawValue = rawValue }

    public static let control = Modifiers(rawValue: 1 << 0)
    public static let option = Modifiers(rawValue: 1 << 1)
    public static let shift = Modifiers(rawValue: 1 << 2)
    public static let command = Modifiers(rawValue: 1 << 3)
}

public struct KeyCombo: Codable, Hashable, Sendable {
    public var key: String
    public var modifiers: Modifiers

    public init(_ key: String, _ modifiers: Modifiers) {
        self.key = key.lowercased()
        self.modifiers = modifiers
    }

    public static let namedKeys: Set<String> = ["space", "delete", "return", "escape", "tab"]

    public var isBindable: Bool { Self.namedKeys.contains(key) || key.count == 1 }

    public var display: String {
        var text = ""
        if modifiers.contains(.control) { text += "⌃" }
        if modifiers.contains(.option) { text += "⌥" }
        if modifiers.contains(.shift) { text += "⇧" }
        if modifiers.contains(.command) { text += "⌘" }
        return text + Self.name(of: key)
    }

    private static func name(of key: String) -> String {
        switch key {
        case "space": "Space"
        case "delete": "⌫"
        case "return": "↩"
        case "escape": "⎋"
        case "tab": "⇥"
        default: key.uppercased()
        }
    }
}

public enum ShortcutAction: String, CaseIterable, Codable, CodingKeyRepresentable, Sendable, Identifiable {
    case nextWaiting
    case lastSession
    case filter
    case newSession
    case linearLauncher
    case review
    case shellSplit
    case shellPopup
    case nvimAtFile
    case rename
    case model
    case effort
    case mute
    case endSession
    case resumeEnded
    case worktrees
    case killDevServers
    case inspector
    case backToSidebar

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .nextWaiting: "Next waiting session"
        case .lastSession: "Last session"
        case .filter: "Filter sessions"
        case .newSession: "New session"
        case .linearLauncher: "Linear launcher"
        case .review: "Review"
        case .shellSplit: "Shell in worktree (split)"
        case .shellPopup: "Shell in worktree (popup)"
        case .nvimAtFile: "nvim at file"
        case .rename: "Rename and pin"
        case .model: "Model"
        case .effort: "Effort"
        case .mute: "Mute"
        case .endSession: "End session"
        case .resumeEnded: "Resume ended"
        case .worktrees: "Worktrees and disk"
        case .killDevServers: "Kill session's dev servers"
        case .inspector: "Inspector"
        case .backToSidebar: "Back to sidebar from terminal"
        }
    }

    public var tuiKey: String? {
        switch self {
        case .nextWaiting: "space"
        case .newSession: "n"
        case .linearLauncher: "L"
        case .review: "r"
        case .shellSplit: "t"
        case .shellPopup: "T"
        case .nvimAtFile: "e"
        case .rename: "R"
        case .model: "M"
        case .effort: "E"
        case .mute: "m"
        case .endSession: "x y"
        case .resumeEnded: "u"
        case .worktrees: "w"
        case .killDevServers: "K"
        case .backToSidebar: "ctrl+\\"
        case .lastSession, .filter, .inspector: nil
        }
    }

    public var defaultCombo: KeyCombo {
        switch self {
        case .nextWaiting: KeyCombo("space", [.control])
        case .lastSession: KeyCombo("[", [.command])
        case .filter: KeyCombo("k", [.command])
        case .newSession: KeyCombo("n", [.command])
        case .linearLauncher: KeyCombo("n", [.command, .shift])
        case .review: KeyCombo("r", [.command])
        case .shellSplit: KeyCombo("t", [.command])
        case .shellPopup: KeyCombo("t", [.command, .shift])
        case .nvimAtFile: KeyCombo("e", [.command])
        case .rename: KeyCombo("r", [.command, .shift])
        case .model: KeyCombo("m", [.control, .command])
        case .effort: KeyCombo("e", [.control, .command])
        case .mute: KeyCombo("m", [.option, .command])
        case .endSession: KeyCombo("delete", [.command])
        case .resumeEnded: KeyCombo("u", [.command, .shift])
        case .worktrees: KeyCombo("w", [.command, .shift])
        case .killDevServers: KeyCombo("k", [.option, .command])
        case .inspector: KeyCombo("i", [.option, .command])
        case .backToSidebar: KeyCombo("0", [.command])
        }
    }
}

public enum ShortcutRefusal: Error, Equatable, Sendable {
    case taken(by: String)
    case needsModifier
    case unsupportedKey

    public func message(for combo: KeyCombo) -> String {
        switch self {
        case .unsupportedKey: "\(combo.display) is not a key agentws can bind"
        case .taken(let holder): "\(combo.display) is already used by \(holder)"
        case .needsModifier: "\(combo.display) needs ⌘ or ⌃, so the terminal keeps plain keys"
        }
    }
}

public struct Shortcuts: Codable, Equatable, Sendable {
    public static let sessionJumps = "Jump to session N"

    public static let fixed: [KeyCombo: String] = [
        KeyCombo("s", [.control, .command]): "Show the sidebar in review",
        KeyCombo("return", [.shift, .command]): "Send review",
        KeyCombo(",", [.command]): "Settings",
    ]

    public var passThrough = true
    private var bindings: [ShortcutAction: KeyCombo?]

    public static var defaults: Shortcuts {
        Shortcuts(bindings: Dictionary(uniqueKeysWithValues: ShortcutAction.allCases.map { ($0, $0.defaultCombo) }))
    }

    private init(bindings: [ShortcutAction: KeyCombo?]) {
        self.bindings = bindings
    }

    public func combo(for action: ShortcutAction) -> KeyCombo? {
        guard let bound = bindings[action] else { return action.defaultCombo }
        return bound
    }

    public var bound: Set<KeyCombo> {
        Set(ShortcutAction.allCases.compactMap { combo(for: $0) })
    }

    public func holder(of combo: KeyCombo) -> ShortcutAction? {
        ShortcutAction.allCases.first { self.combo(for: $0) == combo }
    }

    public mutating func assign(_ combo: KeyCombo?, to action: ShortcutAction) throws(ShortcutRefusal) {
        guard let combo else {
            bindings[action] = .some(nil)
            return
        }
        if !combo.isBindable { throw .unsupportedKey }
        if !combo.modifiers.contains(.command) && !combo.modifiers.contains(.control) { throw .needsModifier }
        if combo.modifiers == [.command], combo.key.count == 1, ("1"..."9").contains(combo.key) {
            throw .taken(by: Self.sessionJumps)
        }
        if let fixed = Self.fixed[combo] { throw .taken(by: fixed) }
        if let holder = holder(of: combo), holder != action { throw .taken(by: holder.title) }
        bindings[action] = combo
    }

    public mutating func restoreDefault(_ action: ShortcutAction) {
        try? assign(action.defaultCombo, to: action)
    }

    public mutating func restoreDefaults() {
        bindings = Self.defaults.bindings
    }
}
