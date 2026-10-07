import Foundation
import Observation

public enum SettingsTab: String, CaseIterable, Sendable, Identifiable {
    case general
    case workspaces
    case agents
    case notifications
    case appearance
    case shortcuts

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .general: "General"
        case .workspaces: "Workspaces"
        case .agents: "Agents"
        case .notifications: "Notifications"
        case .appearance: "Appearance"
        case .shortcuts: "Shortcuts"
        }
    }

    public var symbol: String {
        switch self {
        case .general: "gearshape"
        case .workspaces: "folder"
        case .agents: "cpu"
        case .notifications: "bell"
        case .appearance: "paintpalette"
        case .shortcuts: "keyboard"
        }
    }

    public var perServer: Bool { self == .workspaces || self == .agents }
}

public enum BadgeCount: String, Codable, CaseIterable, Sendable {
    case needsYou
    case needsYouAndDone
    case none

    public var title: String {
        switch self {
        case .needsYou: "Sessions that need you"
        case .needsYouAndDone: "Need you and done unread"
        case .none: "Nothing"
        }
    }
}

public struct General: Codable, Equatable, Sendable {
    public var openAtLogin = false
    public var launchServer = "This Mac"
    public var menuBarCount = BadgeCount.needsYouAndDone
    public var dockBadge = BadgeCount.needsYou
    public var endedShown = 10
    public var confirmOnEnd = true
    public var showWorktrees = true
    public var showSubagents = false
    public var checkUpdates = true

    public init() {}

    public init(
        openAtLogin: Bool, launchServer: String, menuBarCount: BadgeCount, dockBadge: BadgeCount,
        endedShown: Int, confirmOnEnd: Bool, showWorktrees: Bool, showSubagents: Bool, checkUpdates: Bool
    ) {
        self.openAtLogin = openAtLogin
        self.launchServer = launchServer
        self.menuBarCount = menuBarCount
        self.dockBadge = dockBadge
        self.endedShown = endedShown
        self.confirmOnEnd = confirmOnEnd
        self.showWorktrees = showWorktrees
        self.showSubagents = showSubagents
        self.checkUpdates = checkUpdates
    }
}

public struct EventAlert: Codable, Equatable, Sendable {
    public var banner: Bool
    public var sound: Bool

    public init(banner: Bool, sound: Bool) {
        self.banner = banner
        self.sound = sound
    }
}

public struct NotificationSettings: Codable, Equatable, Sendable {
    public var permission = EventAlert(banner: true, sound: true)
    public var waiting = EventAlert(banner: true, sound: false)
    public var done = EventAlert(banner: true, sound: false)
    public var limitOrError = EventAlert(banner: true, sound: false)
    public var skipSessionInView = true
    public var notifyMuted = false

    public init() {}

    public init(
        permission: EventAlert, waiting: EventAlert, done: EventAlert, limitOrError: EventAlert,
        skipSessionInView: Bool, notifyMuted: Bool
    ) {
        self.permission = permission
        self.waiting = waiting
        self.done = done
        self.limitOrError = limitOrError
        self.skipSessionInView = skipSessionInView
        self.notifyMuted = notifyMuted
    }
}

public enum ThemeChoice: String, Codable, CaseIterable, Sendable {
    case system
    case latte
    case mocha

    public var title: String {
        switch self {
        case .system: "Match system"
        case .latte: "Latte"
        case .mocha: "Mocha"
        }
    }

    public func palette(systemDark: Bool) -> Palette {
        switch self {
        case .system: systemDark ? .mocha : .latte
        case .latte: .latte
        case .mocha: .mocha
        }
    }
}

public enum AccentChoice: String, Codable, CaseIterable, Sendable {
    case blue
    case peach
    case green
    case red

    public var tone: Tone {
        switch self {
        case .blue: .blue
        case .peach: .peach
        case .green: .green
        case .red: .red
        }
    }
}

public enum CursorShape: String, Codable, CaseIterable, Sendable {
    case block
    case bar
    case underline
}

public enum Density: String, Codable, CaseIterable, Sendable {
    case regular
    case compact
}

public enum DiffLayout: String, Codable, CaseIterable, Sendable {
    case unified
    case split
}

public struct Appearance: Codable, Equatable, Sendable {
    public static let fontSizes = 9.0...24.0
    public static let lineHeights = 1.0...2.0

    public var theme = ThemeChoice.system
    public var accent = AccentChoice.blue
    public var terminalFont = "SF Mono"
    public var fontSize = 12.0 { didSet { fontSize = fontSize.clamped(to: Self.fontSizes) } }
    public var lineHeight = 1.2 { didSet { lineHeight = lineHeight.clamped(to: Self.lineHeights) } }
    public var cursor = CursorShape.block
    public var cursorBlinks = true
    public var density = Density.regular
    public var diffLayout = DiffLayout.unified

    public init() {}

    public init(
        theme: ThemeChoice, accent: AccentChoice, terminalFont: String, fontSize: Double, lineHeight: Double,
        cursor: CursorShape, cursorBlinks: Bool, density: Density, diffLayout: DiffLayout
    ) {
        self.theme = theme
        self.accent = accent
        self.terminalFont = terminalFont
        self.fontSize = fontSize.clamped(to: Self.fontSizes)
        self.lineHeight = lineHeight.clamped(to: Self.lineHeights)
        self.cursor = cursor
        self.cursorBlinks = cursorBlinks
        self.density = density
        self.diffLayout = diffLayout
    }
}

extension Double {
    func clamped(to range: ClosedRange<Double>) -> Double { Swift.min(Swift.max(self, range.lowerBound), range.upperBound) }
}

public struct AppSettings: Equatable, Sendable {
    public var general = General()
    public var notifications = NotificationSettings()
    public var appearance = Appearance()
    public var shortcuts = Shortcuts.defaults

    public init() {}
}

@MainActor
@Observable
public final class SettingsStore {
    public enum Key {
        public static let general = "settings.general"
        public static let notifications = "settings.notifications"
        public static let appearance = "settings.appearance"
        public static let shortcuts = "settings.shortcuts"
    }

    public var settings: AppSettings {
        didSet { save() }
    }

    @ObservationIgnored private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        var loaded = AppSettings()
        Self.load(defaults, Key.general, into: &loaded.general)
        Self.load(defaults, Key.notifications, into: &loaded.notifications)
        Self.load(defaults, Key.appearance, into: &loaded.appearance)
        Self.load(defaults, Key.shortcuts, into: &loaded.shortcuts)
        settings = loaded
    }

    private static func load<Value: Decodable>(_ defaults: UserDefaults, _ key: String, into value: inout Value) {
        guard let data = defaults.data(forKey: key), let decoded = try? JSONDecoder().decode(Value.self, from: data) else { return }
        value = decoded
    }

    private func save() {
        store(settings.general, Key.general)
        store(settings.notifications, Key.notifications)
        store(settings.appearance, Key.appearance)
        store(settings.shortcuts, Key.shortcuts)
    }

    private func store<Value: Encodable>(_ value: Value, _ key: String) {
        guard let data = try? JSONEncoder().encode(value) else { return }
        defaults.set(data, forKey: key)
    }
}

public enum CLILinkStatus: Equatable, Sendable {
    case missing
    case linked
    case elsewhere(String)
}

public struct CLILinkError: Error, Equatable, Sendable {
    public var message: String
}

public struct CLILink: Sendable {
    public static let standardPath = "/usr/local/bin/agentws"

    public var path: String
    public var target: String

    public init(path: String = CLILink.standardPath, target: String) {
        self.path = path
        self.target = target
    }

    public var status: CLILinkStatus {
        let files = FileManager.default
        if let destination = try? files.destinationOfSymbolicLink(atPath: path) {
            return destination == target ? .linked : .elsewhere(destination)
        }
        return files.fileExists(atPath: path) ? .elsewhere(path) : .missing
    }

    public func install() throws {
        switch status {
        case .linked:
            return
        case .elsewhere(let other):
            throw CLILinkError(message: "\(path) already points at \(other); remove it first")
        case .missing:
            guard target.hasPrefix("/") else { throw CLILinkError(message: "this app has no bundled agentws to link") }
            let files = FileManager.default
            try files.createDirectory(atPath: (path as NSString).deletingLastPathComponent, withIntermediateDirectories: true)
            try files.createSymbolicLink(atPath: path, withDestinationPath: target)
        }
    }

    public func remove() throws {
        switch status {
        case .missing:
            return
        case .elsewhere(let other):
            throw CLILinkError(message: "\(path) points at \(other), not this app's agentws; left alone")
        case .linked:
            try FileManager.default.removeItem(atPath: path)
        }
    }
}
