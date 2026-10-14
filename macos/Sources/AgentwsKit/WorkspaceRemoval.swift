import Foundation

public struct WorkspaceRemoval: Sendable, Equatable, Identifiable {
    public var root: String
    public var live: Int

    public var id: String { root }

    public init(root: String, sessions: [Session]) {
        self.root = root
        let prefix = root.hasSuffix("/") ? root : root + "/"
        live = sessions.filter { !$0.ended && ($0.dir == root || $0.dir.hasPrefix(prefix)) }.count
    }

    public var title: String { "Remove \(root) from agentws?" }

    public var button: String { "Remove" }

    public var message: String {
        let safe = "agentws stops using this workspace. No files, worktrees or branches on the server are deleted."
        switch live {
        case 0: return safe
        case 1: return "1 live session uses this workspace. " + safe
        default: return "\(live) live sessions use this workspace. " + safe
        }
    }
}
