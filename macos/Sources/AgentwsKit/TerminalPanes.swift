import Foundation

public struct NativeClientParams: Encodable, Sendable {
    public var cols: Int
    public var rows: Int

    public init(cols: Int, rows: Int) {
        self.cols = cols
        self.rows = rows
    }
}

public struct NativeClient: Decodable, Sendable, Equatable {
    public var argv: [String]
    public var session: String
    public var panes: [PaneState]

    public init(argv: [String], session: String, panes: [PaneState]) {
        self.argv = argv
        self.session = session
        self.panes = panes
    }

    private enum CodingKeys: String, CodingKey { case argv, session, panes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        argv = try c.decode([String].self, forKey: .argv)
        session = try c.decode(String.self, forKey: .session)
        panes = try c.decodeIfPresent([PaneState].self, forKey: .panes) ?? []
    }
}

public struct Cursor: Equatable, Sendable {
    public var x: Int
    public var y: Int

    public init(x: Int, y: Int) {
        self.x = x
        self.y = y
    }
}

public struct PaneModes: Equatable, Sendable {
    public var mouseAny = false
    public var mouseButton = false
    public var mouseStandard = false
    public var mouseSGR = false
    public var alternate = false
    public var cursorVisible = false
    public var cursorKeys = false

    public init(
        mouseAny: Bool = false, mouseButton: Bool = false, mouseStandard: Bool = false, mouseSGR: Bool = false,
        alternate: Bool = false, cursorVisible: Bool = false, cursorKeys: Bool = false
    ) {
        self.mouseAny = mouseAny
        self.mouseButton = mouseButton
        self.mouseStandard = mouseStandard
        self.mouseSGR = mouseSGR
        self.alternate = alternate
        self.cursorVisible = cursorVisible
        self.cursorKeys = cursorKeys
    }

    var restore: String {
        var out = ""
        if cursorKeys { out += "\u{1b}[?1h" }
        if mouseStandard { out += "\u{1b}[?1000h" }
        if mouseButton { out += "\u{1b}[?1002h" }
        if mouseAny { out += "\u{1b}[?1003h" }
        if mouseSGR { out += "\u{1b}[?1006h" }
        if !cursorVisible { out += "\u{1b}[?25l" }
        return out
    }
}

public struct PaneState: Decodable, Equatable, Sendable {
    public var pane: String
    public var window: String
    public var cols: Int
    public var rows: Int
    public var cursor: Cursor?
    public var modes: PaneModes

    public init(pane: String, window: String, cols: Int, rows: Int, cursor: Cursor? = nil, modes: PaneModes = PaneModes()) {
        self.pane = pane
        self.window = window
        self.cols = cols
        self.rows = rows
        self.cursor = cursor
        self.modes = modes
    }

    public init?(query line: String) {
        let f = line.split(separator: " ").map(String.init)
        guard f.count == 13, f[0].hasPrefix("%"), f[1].hasPrefix("@") else { return nil }
        let n = f[2...].compactMap { Int($0) }
        guard n.count == 11 else { return nil }
        self.init(pane: f[0], window: f[1], cols: n[0], rows: n[1], cursor: Cursor(x: n[2], y: n[3]), modes: PaneModes(
            mouseAny: n[4] == 1, mouseButton: n[5] == 1, mouseStandard: n[6] == 1, mouseSGR: n[7] == 1,
            alternate: n[8] == 1, cursorVisible: n[9] == 1, cursorKeys: n[10] == 1
        ))
    }

    private enum CodingKeys: String, CodingKey {
        case pane, window, cols, rows
        case mouseAny = "mouse_any", mouseButton = "mouse_button", mouseStandard = "mouse_standard", mouseSGR = "mouse_sgr"
        case alternate, cursorVisible = "cursor_visible", cursorKeys = "cursor_keys"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        func flag(_ key: CodingKeys) throws -> Bool { try c.decodeIfPresent(Bool.self, forKey: key) ?? false }
        self.init(
            pane: try c.decode(String.self, forKey: .pane), window: try c.decode(String.self, forKey: .window),
            cols: try c.decode(Int.self, forKey: .cols), rows: try c.decode(Int.self, forKey: .rows),
            modes: PaneModes(
                mouseAny: try flag(.mouseAny), mouseButton: try flag(.mouseButton), mouseStandard: try flag(.mouseStandard),
                mouseSGR: try flag(.mouseSGR), alternate: try flag(.alternate), cursorVisible: try flag(.cursorVisible),
                cursorKeys: try flag(.cursorKeys)
            )
        )
    }

    public func prime(history: [String]) -> [UInt8] {
        var out = "\u{1b}c"
        if modes.alternate { out += "\u{1b}[?1049h\u{1b}[H" }
        out += history.joined(separator: "\r\n")
        out += "\u{1b}[0m"
        if let cursor { out += "\u{1b}[\(cursor.y + 1);\(cursor.x + 1)H" }
        out += modes.restore
        return Array(out.utf8)
    }
}

public struct Letterbox: Equatable, Sendable {
    public var cols: Int
    public var rows: Int

    public init?(paneCols: Int, paneRows: Int, viewCols: Int, viewRows: Int) {
        guard paneCols != viewCols || paneRows != viewRows else { return nil }
        cols = paneCols
        rows = paneRows
    }

    public var note: String { "The TUI is showing this pane at \(cols)×\(rows)" }
}

public enum PauseAction: Equatable, Sendable {
    case none
    case resumeAndRedraw
    case stayPaused
}

public struct TerminalPanes: Equatable, Sendable {
    private var states: [String: PaneState] = [:]
    private var visible: Set<String> = []
    private var paused: Set<String> = []

    public init() {}

    public subscript(pane: String) -> PaneState? { states[pane] }

    public mutating func set(_ state: PaneState) {
        states[state.pane] = state
    }

    @discardableResult
    public mutating func apply(_ event: ControlEvent) -> [String] {
        switch event {
        case let .layout(_, sizes):
            for size in sizes where states[size.pane] != nil {
                states[size.pane]?.cols = size.cols
                states[size.pane]?.rows = size.rows
            }
            return []
        case let .windowClose(window):
            let gone = states.values.filter { $0.window == window }.map(\.pane).sorted()
            for pane in gone {
                states[pane] = nil
                visible.remove(pane)
                paused.remove(pane)
            }
            return gone
        case let .resume(pane):
            paused.remove(pane)
            return []
        default:
            return []
        }
    }

    @discardableResult
    public mutating func show(_ pane: String) -> PauseAction {
        visible.insert(pane)
        return paused.remove(pane) == nil ? .none : .resumeAndRedraw
    }

    public mutating func hide(_ pane: String) {
        visible.remove(pane)
    }

    public mutating func pause(_ pane: String) -> PauseAction {
        if visible.contains(pane) { return .resumeAndRedraw }
        paused.insert(pane)
        return .stayPaused
    }

    public mutating func reset() {
        paused = []
    }
}
