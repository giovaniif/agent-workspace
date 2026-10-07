import Foundation

extension ViewState {
    public static let eventsKept = 20

    public var sortedSessions: [Session] {
        sessions.filter { $0.order >= 0 }.sorted { $0.order < $1.order }
    }

    public mutating func apply(_ diff: ViewDiff) {
        seq = diff.seq
        if let w = diff.workspace { Self.upsert(&workspaces, w) { $0.root == w.root } }
        if let root = diff.removedWorkspace { workspaces.removeAll { $0.root == root } }
        if let t = diff.task { Self.upsert(&tasks, t) { $0.id == t.id } }
        if let w = diff.worktree { Self.upsert(&worktrees, w) { $0.id == w.id } }
        if let id = diff.removedWorktree { worktrees.removeAll { $0.id == id } }
        if let s = diff.session { Self.upsert(&sessions, s) { $0.id == s.id } }
        if let id = diff.removedSession {
            sessions.removeAll { $0.id == id }
            events.removeAll { $0.sessionID == id }
            subagents.removeAll { $0.sessionID == id }
            drafts.removeAll { $0.session == id }
        }
        if let l = diff.limits { limits = l }
        if let q = diff.queue { queue = q }
        if let s = diff.sends { sends = s }
        if let e = diff.event { append(e) }
        if let a = diff.subagent { Self.upsert(&subagents, a) { $0.sessionID == a.sessionID && $0.id == a.id } }
        if let d = diff.draft { Self.upsert(&drafts, d) { $0.session == d.session } }
    }

    private mutating func append(_ event: SessionEvent) {
        events.append(event)
        let mine = events.indices.filter { events[$0].sessionID == event.sessionID }
        let excess = mine.count - Self.eventsKept
        if excess > 0 {
            let drop = Set(mine.prefix(excess))
            events = events.enumerated().filter { !drop.contains($0.offset) }.map(\.element)
        }
    }

    private static func upsert<T>(_ list: inout [T], _ value: T, where matches: (T) -> Bool) {
        if let i = list.firstIndex(where: matches) {
            list[i] = value
        } else {
            list.append(value)
        }
    }
}
