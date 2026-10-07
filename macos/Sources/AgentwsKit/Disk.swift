import Foundation

public enum CleanupAction: String, Codable, Sendable, Equatable {
    case remove
    case backupThenAsk = "backup_then_ask"
    case keep
}

public struct DiskRow: Codable, Sendable, Equatable {
    public var worktreeID: String
    public var size: Int64
    public var action: CleanupAction
    public var reason: String

    enum CodingKeys: String, CodingKey {
        case worktreeID = "WorktreeID", size = "Size", action = "Action", reason = "Reason"
    }
}

public struct DepsStore: Codable, Sendable, Equatable {
    public var path: String
    public var size: Int64
}

public struct RecentCleanup: Codable, Sendable, Equatable {
    public var at: String
    public var path: String
    public var branch: String?
    public var action: CleanupAction
    public var outcome: String
}

public struct DiskView: Codable, Sendable, Equatable {
    public var free: UInt64
    public var total: UInt64
    public var autoCleanEveryNanos: Int64
    public var depsStore: DepsStore?
    @NullAsEmpty public var rows: [DiskRow]
    @NullAsEmpty public var recent: [RecentCleanup]
    public var reclaimable: Int64
    public var reclaimablePending: Int
    public var worktreesSize: Int64
    public var worktreesPending: Int

    public var autoCleanEvery: Duration { .nanoseconds(autoCleanEveryNanos) }

    enum CodingKeys: String, CodingKey {
        case free, total, autoCleanEveryNanos = "auto_clean_every", depsStore = "deps_store", rows, recent
        case reclaimable, reclaimablePending = "reclaimable_pending"
        case worktreesSize = "worktrees_size", worktreesPending = "worktrees_pending"
    }
}

public struct CleanupWorktreeParams: Codable, Sendable, Equatable {
    public var path: String
    public var backup: Bool

    public init(path: String, backup: Bool) {
        self.path = path
        self.backup = backup
    }
}

public struct PortsKillParams: Codable, Sendable, Equatable {
    public var pgids: [Int]

    public init(pgids: [Int]) {
        self.pgids = pgids
    }
}

public struct CleanupItem: Codable, Sendable, Equatable {
    public var path: String
    public var branch: String?
    public var action: CleanupAction
    public var reason: String
    public var outcome: String?
}

public struct PortsKilled: Codable, Sendable, Equatable {
    @NullAsEmpty public var killed: [Int]
}

public struct DiskTiles: Sendable, Equatable {
    public var free: String
    public var worktrees: String
    public var reclaimable: String
    public var depsStore: String?
    public var schedule: String

    public init(_ view: DiskView) {
        free = Self.size(Int64(clamping: view.free)) + " free"
        worktrees = Self.measured(view.worktreesSize, pending: view.worktreesPending)
        reclaimable = Self.measured(view.reclaimable, pending: view.reclaimablePending)
        depsStore = view.depsStore.map { Self.size($0.size) }
        let seconds = Int(view.autoCleanEvery.components.seconds)
        schedule = seconds > 0 ? "every " + Self.every(seconds) : "off"
    }

    static func measured(_ bytes: Int64, pending: Int) -> String {
        if pending == 0 { return size(bytes) }
        return bytes == 0 ? "…" : size(bytes) + "+"
    }

    static func every(_ seconds: Int) -> String {
        if seconds % 3_600 == 0 { return "\(seconds / 3_600)h" }
        if seconds % 60 == 0 { return "\(seconds / 60)m" }
        return "\(seconds)s"
    }

    public static func size(_ bytes: Int64) -> String {
        if bytes < 0 { return "…" }
        let units = ["B", "KB", "MB", "GB", "TB"]
        var value = Double(bytes)
        var unit = 0
        while value >= 1_000 && unit < units.count - 1 {
            value /= 1_000
            unit += 1
        }
        if unit == 0 || value >= 10 { return "\(Int(value.rounded())) \(units[unit])" }
        let tenths = (value * 10).rounded() / 10
        return tenths == tenths.rounded() ? "\(Int(tenths)) \(units[unit])" : "\(tenths) \(units[unit])"
    }
}

public struct DiskTableRow: Sendable, Equatable, Identifiable {
    public var id: String
    public var path: String
    public var worktree: String
    public var sessionID: String
    public var session: String
    public var pr: String
    public var status: String
    public var tone: Tone
    public var size: String
    public var ports: String
    public var pgids: [Int]
    public var plan: String
    public var action: CleanupAction
}

public enum DiskTable {
    public static func rows(_ view: DiskView, state: ViewState?) -> [DiskTableRow] {
        let worktrees = Dictionary((state?.worktrees ?? []).map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        let sessions = Dictionary((state?.sessions ?? []).map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        return view.rows.map { row in
            let w = worktrees[row.worktreeID]
            let path = w?.path ?? row.worktreeID
            let ports = w?.ports ?? []
            let detached = w.map { $0.branch.isEmpty } ?? false
            let (status, tone) = Self.status(row.action, pr: w?.pr, detached: detached)
            return DiskTableRow(
                id: row.worktreeID,
                path: path,
                worktree: URL(fileURLWithPath: path).lastPathComponent,
                sessionID: w?.sessionID ?? "",
                session: w.flatMap { sessions[$0.sessionID]?.name } ?? "",
                pr: w?.pr.map { "#\($0.number)" } ?? "",
                status: status,
                tone: tone,
                size: DiskTiles.size(row.size),
                ports: ports.map { ":\($0.port)" }.joined(separator: " "),
                pgids: Array(Set(ports.map(\.pgid))).sorted(),
                plan: Self.plan(row, detached: detached),
                action: row.action
            )
        }
    }

    static func status(_ action: CleanupAction, pr: PullRequest?, detached: Bool) -> (String, Tone) {
        switch action {
        case .remove: return ("Merged · clean", .green)
        case .backupThenAsk: return detached ? ("Detached", .peach) : ("Merged · dirty", .peach)
        case .keep: break
        }
        if detached { return ("Detached", .grey) }
        guard let pr else { return ("No PR", .grey) }
        return pr.state == "MERGED" ? ("Merged · clean", .green) : ("Open", .blue)
    }

    static func plan(_ row: DiskRow, detached: Bool) -> String {
        switch row.action {
        case .remove: return "removes in the next cleanup"
        case .backupThenAsk: return detached ? "backup branch first" : "back up then ask"
        case .keep: return "kept: " + row.reason
        }
    }
}

public struct DiskAction: Sendable, Equatable, Identifiable {
    public enum Kind: Sendable, Equatable {
        case goToSession
        case openShell
        case killDevServers
        case remove
        case backUpAndRemove

        public var destructive: Bool {
            switch self {
            case .killDevServers, .remove, .backUpAndRemove: return true
            case .goToSession, .openShell: return false
            }
        }
    }

    public var id: String { title }
    public var kind: Kind
    public var title: String
    public var outcome: String
    public var confirm: String?
    public var params: CleanupWorktreeParams?
    public var pgids: [Int]
}

public enum DiskActions {
    public static func `for`(_ row: DiskTableRow) -> [DiskAction] {
        var out: [DiskAction] = []
        if !row.sessionID.isEmpty {
            out.append(DiskAction(kind: .goToSession, title: "Go to session", outcome: "Shows \(row.session.isEmpty ? "its session" : row.session) in the main window.", confirm: nil, params: nil, pgids: []))
        }
        out.append(DiskAction(kind: .openShell, title: "Open shell", outcome: "Opens a shell in \(row.worktree).", confirm: nil, params: nil, pgids: []))
        if !row.pgids.isEmpty {
            let servers = row.ports.contains(" ") ? "dev servers on \(row.ports)" : "dev server on \(row.ports)"
            out.append(DiskAction(
                kind: .killDevServers, title: "Kill dev servers", outcome: "Stops the \(servers).",
                confirm: "Stop the \(servers) in \(row.worktree)?", params: nil, pgids: row.pgids
            ))
        }
        switch row.action {
        case .remove:
            out.append(DiskAction(
                kind: .remove, title: "Remove…", outcome: "Moves \(row.worktree) to the trash and keeps its branch.",
                confirm: "Remove \(row.worktree)? It goes to the agentws trash; the branch stays.",
                params: CleanupWorktreeParams(path: row.path, backup: false), pgids: []
            ))
        case .backupThenAsk:
            out.append(DiskAction(
                kind: .backUpAndRemove, title: "Back up and remove…",
                outcome: "Backs up the uncommitted changes in \(row.worktree), then moves it to the trash.",
                confirm: "Back up \(row.worktree) and remove it? The backup goes to the agentws backups folder.",
                params: CleanupWorktreeParams(path: row.path, backup: true), pgids: []
            ))
        case .keep:
            break
        }
        return out
    }
}

public struct DiskPortRow: Sendable, Equatable, Identifiable {
    public var id: String { "\(port)-\(pgid)" }
    public var port: Int
    public var pgid: Int
    public var command: String
    public var worktree: String
    public var session: String
}

public enum DiskPorts {
    public static func rows(_ state: ViewState?) -> [DiskPortRow] {
        guard let state else { return [] }
        let names = Dictionary(state.sessions.map { ($0.id, $0.name) }, uniquingKeysWith: { a, _ in a })
        return state.worktrees.flatMap { w in
            (w.ports ?? []).map { p in
                DiskPortRow(port: p.port, pgid: p.pgid, command: p.command, worktree: URL(fileURLWithPath: w.path).lastPathComponent, session: names[w.sessionID] ?? "")
            }
        }.sorted { $0.port < $1.port }
    }
}

public struct DiskRecentRow: Sendable, Equatable, Identifiable {
    public var id: String { at + path }
    public var at: String
    public var path: String
    public var worktree: String
    public var branch: String
    public var when: String
    public var outcome: String
}

public enum DiskRecent {
    public static func rows(_ view: DiskView, now: Date) -> [DiskRecentRow] {
        view.recent.map { r in
            let when = Timestamp.parse(r.at).map { Header.elapsed(Int(now.timeIntervalSince($0))) + " ago" } ?? ""
            return DiskRecentRow(at: r.at, path: r.path, worktree: URL(fileURLWithPath: r.path).lastPathComponent, branch: r.branch ?? "", when: when, outcome: r.outcome)
        }
    }
}

public struct ShellParams: Codable, Sendable, Equatable {
    public var session: String
    public var worktree: String

    public init(session: String, worktree: String) {
        self.session = session
        self.worktree = worktree
    }
}

public struct ShellResult: Codable, Sendable, Equatable {
    public var pane: String?
}

extension Seed {
    public static func disk(measuring: Bool = false) -> DiskView {
        let size = { (bytes: Int64) in measuring ? -1 : bytes }
        let rows: [DiskRow] = [
            DiskRow(worktreeID: "w1", size: 412_000_000, action: .keep, reason: "its session is live"),
            DiskRow(worktreeID: "w2", size: size(1_240_000_000), action: .keep, reason: "its session is live"),
            DiskRow(worktreeID: "w3", size: 88_000_000, action: .keep, reason: "not merged"),
            DiskRow(worktreeID: "w4", size: size(36_000_000), action: .remove, reason: "merged into the default branch"),
            DiskRow(worktreeID: "w6", size: 905_000_000, action: .keep, reason: "in use by node"),
            DiskRow(worktreeID: "w7", size: 214_000_000, action: .remove, reason: "PR #77 merged"),
            DiskRow(worktreeID: "w8", size: size(530_000_000), action: .backupThenAsk, reason: "3 uncommitted changes"),
        ]
        let reclaimable = rows.filter { $0.action != .keep }
        let stamp = ISO8601DateFormatter()
        return DiskView(
            free: 61_400_000_000,
            total: 494_000_000_000,
            autoCleanEveryNanos: 600_000_000_000,
            depsStore: DepsStore(path: "/Users/me/Library/pnpm/store", size: size(3_100_000_000)),
            rows: rows,
            recent: [
                RecentCleanup(at: stamp.string(from: now.addingTimeInterval(-1_500)), path: "/w/api-old-auth", branch: "old-auth", action: .remove, outcome: "removed"),
                RecentCleanup(at: stamp.string(from: now.addingTimeInterval(-9_000)), path: "/w/web-tweak", branch: "tweak", action: .backupThenAsk, outcome: "backed up, removed"),
            ],
            reclaimable: reclaimable.filter { $0.size >= 0 }.reduce(0) { $0 + $1.size },
            reclaimablePending: reclaimable.filter { $0.size < 0 }.count,
            worktreesSize: rows.filter { $0.size >= 0 }.reduce(0) { $0 + $1.size },
            worktreesPending: rows.filter { $0.size < 0 }.count
        )
    }
}
