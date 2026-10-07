import Foundation

public struct SidebarRow: Sendable, Equatable, Identifiable {
    public var id: String
    public var name: String
    public var bold: Bool
    public var muted: Bool
    public var badge: String
    public var `where`: String
    public var detail: String
    public var style: StateStyle
    public var needsYou: Bool
    public var shortcut: String?
    public var worktrees: [String]
}

public struct Sidebar: Sendable, Equatable {
    public var rows: [SidebarRow]
    public var ended: [SidebarRow]
    public var footer: String

    public init(state: ViewState, filter: String = "") {
        let worktrees = Dictionary(state.worktrees.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        let query = filter.trimmingCharacters(in: .whitespaces).lowercased()
        let matches: (Session) -> Bool = { s in
            query.isEmpty || s.name.lowercased().contains(query) || s.where.lowercased().contains(query)
        }
        let live = state.sortedSessions.filter { !$0.ended }
        let gone = state.sessions.filter(\.ended)
        rows = live.filter(matches).enumerated().map { i, s in
            Self.row(s, worktrees: worktrees, shortcut: i < 9 ? "⌘\(i + 1)" : nil)
        }
        ended = gone.filter(matches).map { Self.row($0, worktrees: worktrees, shortcut: nil) }
        let waiting = live.filter { StateStyle.needsYou($0.state) }.count
        var parts = ["\(live.count) session\(live.count == 1 ? "" : "s")"]
        if waiting > 0 { parts.append("\(waiting) need\(waiting == 1 ? "s" : "") you") }
        if !gone.isEmpty { parts.append("\(gone.count) ended") }
        if let r = state.reclaimable, r.size > 0 || r.pending > 0 {
            parts.append(DiskTiles.measured(r.size, pending: r.pending) + " reclaimable")
        }
        footer = parts.joined(separator: " · ")
    }

    public var all: [SidebarRow] { rows + ended }

    static func row(_ s: Session, worktrees: [String: Worktree], shortcut: String?) -> SidebarRow {
        var detail = [s.model, s.effort].filter { !$0.isEmpty }
        if s.usage.hasContext { detail.append("ctx \(s.usage.contextLeftPercent)%") }
        return SidebarRow(
            id: s.id,
            name: s.name,
            bold: s.unread,
            muted: s.muted,
            badge: s.harness == "codex" ? "CX" : "CC",
            where: s.where,
            detail: detail.joined(separator: " · "),
            style: StateStyle(state: s.state),
            needsYou: StateStyle.needsYou(s.state),
            shortcut: shortcut,
            worktrees: s.worktreeIDs.compactMap { worktrees[$0] }.map(worktreeLine)
        )
    }

    static func worktreeLine(_ w: Worktree) -> String {
        var line = w.repo
        if !w.subtaskSlug.isEmpty { line += ":" + w.subtaskSlug }
        if let pr = w.pr {
            line += " #\(pr.number)"
            let mark = checkMark(pr.checks)
            if !mark.isEmpty { line += " " + mark }
        }
        for port in w.ports ?? [] { line += " :\(port.port)" }
        return line
    }

    public static func checkMark(_ checks: String) -> String {
        switch checks {
        case "passing": "✓"
        case "failing": "✗"
        case "pending": "●"
        default: ""
        }
    }
}

public struct Navigator: Sendable, Equatable {
    public private(set) var selected: String?
    private var previous: String?

    public init() {}

    public mutating func select(_ id: String) {
        guard id != selected else { return }
        previous = selected
        selected = id
    }

    public mutating func jump(to number: Int, in sidebar: Sidebar) {
        guard number >= 1, number <= min(9, sidebar.rows.count) else { return }
        select(sidebar.rows[number - 1].id)
    }

    public mutating func nextWaiting(in sidebar: Sidebar) {
        let rows = sidebar.rows
        let n = rows.count
        guard n > 0 else { return }
        let current = rows.firstIndex { $0.id == selected } ?? -1
        for step in 1...n {
            let i = (current + step + n) % n
            if rows[i].needsYou {
                select(rows[i].id)
                return
            }
        }
    }

    public mutating func last() {
        if let previous { select(previous) }
    }

    public mutating func reconcile(with sidebar: Sidebar) {
        if let selected, sidebar.all.contains(where: { $0.id == selected }) { return }
        selected = sidebar.rows.first?.id
    }
}
