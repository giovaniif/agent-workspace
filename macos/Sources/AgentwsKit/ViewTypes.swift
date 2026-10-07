import Foundation

public struct Workspace: Codable, Sendable, Equatable {
    public var root: String
    public var kind: String
    @NullAsEmpty public var repos: [JSONValue]
    public var lastUsed: String

    enum CodingKeys: String, CodingKey {
        case root = "Root", kind = "Kind", repos = "Repos", lastUsed = "LastUsed"
    }
}

public struct TaskItem: Codable, Sendable, Equatable {
    public var id: String
    public var source: String
    public var ref: String
    public var text: String
    public var issueTitle: String
    public var prTitle: String
    public var pinnedName: String
    public var url: String

    enum CodingKeys: String, CodingKey {
        case id = "ID", source = "Source", ref = "Ref", text = "Text", issueTitle = "IssueTitle"
        case prTitle = "PRTitle", pinnedName = "PinnedName", url = "URL"
    }
}

public struct FailingCheck: Codable, Sendable, Equatable {
    public var name: String
    public var url: String

    enum CodingKeys: String, CodingKey {
        case name = "Name", url = "URL"
    }
}

public struct PullRequest: Codable, Sendable, Equatable {
    public var number: Int
    public var title: String
    public var url: String
    public var head: String
    public var state: String
    public var checks: String
    public var reviewDecision: String
    public var mergeable: String
    public var unresolvedThreads: Int
    public var botComments: Int
    @NullAsEmpty public var failing: [FailingCheck]

    enum CodingKeys: String, CodingKey {
        case number = "Number", title = "Title", url = "URL", head = "Head", state = "State", checks = "Checks"
        case reviewDecision = "ReviewDecision", mergeable = "Mergeable", unresolvedThreads = "UnresolvedThreads"
        case botComments = "BotComments", failing = "Failing"
    }
}

public struct Port: Codable, Sendable, Equatable {
    public var port: Int
    public var pid: Int
    public var pgid: Int
    public var command: String

    enum CodingKeys: String, CodingKey {
        case port = "Port", pid = "PID", pgid = "PGID", command = "Command"
    }
}

public struct Worktree: Codable, Sendable, Equatable {
    public var id: String
    public var repo: String
    public var path: String
    public var branch: String
    @Nullable public var pr: PullRequest?
    public var subtaskSlug: String
    public var sessionID: String
    @Nullable public var ports: [Port]?

    enum CodingKeys: String, CodingKey {
        case id = "ID", repo = "Repo", path = "Path", branch = "Branch", pr = "PR"
        case subtaskSlug = "SubtaskSlug", sessionID = "SessionID", ports = "Ports"
    }
}

public struct Usage: Codable, Sendable, Equatable {
    public var contextLeftPercent: Int
    public var hasContext: Bool
    public var limitUsedPercent: Int

    enum CodingKeys: String, CodingKey {
        case contextLeftPercent = "ContextLeftPercent", hasContext = "HasContext", limitUsedPercent = "LimitUsedPercent"
    }
}

public struct BoardCheck: Codable, Sendable, Equatable {
    public var name: String
    public var url: String
}

public struct BoardPR: Codable, Sendable, Equatable {
    public var number: Int
    public var title: String
    public var url: String
    public var branch: String
    public var state: String
    public var checks: String
    @NullAsEmpty public var failingChecks: [BoardCheck]
    public var reviewDecision: String
    public var unresolvedThreads: Int
    public var botCommentsSincePush: Int
    public var mergeable: String
    public var readyToMerge: Bool
    @NullAsEmpty public var blockers: [String]

    enum CodingKeys: String, CodingKey {
        case number, title, url, branch, state, checks
        case failingChecks = "failing_checks", reviewDecision = "review_decision"
        case unresolvedThreads = "unresolved_threads", botCommentsSincePush = "bot_comments_since_push"
        case mergeable, readyToMerge = "ready_to_merge", blockers
    }
}

public struct Session: Codable, Sendable, Equatable {
    public var id: String
    public var taskID: String
    public var harness: String
    public var pane: String
    public var model: String
    public var effort: String
    public var state: String
    public var ended: Bool
    public var unread: Bool
    public var focused: Bool
    public var muted: Bool
    @NullAsEmpty public var worktreeIDs: [String]
    public var resumeID: String
    public var transcript: String
    public var dir: String
    public var usage: Usage
    @NullAsEmpty public var limits: [JSONValue]
    public var limitsAt: String
    @NullAsEmpty public var switches: [JSONValue]
    public var switchWarning: Bool
    public var name: String
    public var `where`: String
    public var banner: String
    @Nullable public var since: String?
    public var order: Int
    @NullAsEmpty public var board: [BoardPR]

    enum CodingKeys: String, CodingKey {
        case id = "ID", taskID = "TaskID", harness = "Harness", pane = "Pane", model = "Model", effort = "Effort"
        case state = "State", ended = "Ended", unread = "Unread", focused = "Focused", muted = "Muted"
        case worktreeIDs = "WorktreeIDs", resumeID = "ResumeID", transcript = "Transcript", dir = "Dir"
        case usage = "Usage", limits = "Limits", limitsAt = "LimitsAt", switches = "Switches"
        case switchWarning = "SwitchWarning", name, `where`, banner, since, order, board
    }
}

public struct SessionEvent: Codable, Sendable, Equatable {
    public var sessionID: String
    public var at: String
    public var kind: String
    public var tool: String
    public var detail: String
    public var text: String

    enum CodingKeys: String, CodingKey {
        case sessionID = "SessionID", at = "At", kind = "Kind", tool = "Tool", detail = "Detail", text = "Text"
    }
}

public struct Subagent: Codable, Sendable, Equatable {
    public var sessionID: String
    public var id: String
    public var parentID: String
    public var type: String
    public var state: String
    public var startedAt: String
    public var stoppedAt: String
    public var summary: String

    enum CodingKeys: String, CodingKey {
        case sessionID = "SessionID", id = "ID", parentID = "ParentID", type = "Type", state = "State"
        case startedAt = "StartedAt", stoppedAt = "StoppedAt", summary = "Summary"
    }
}

public struct QueuedSend: Codable, Sendable, Equatable {
    public var id: String
    public var session: String
    public var text: String
    public var queuedAt: String

    enum CodingKeys: String, CodingKey {
        case id, session, text, queuedAt = "queued_at"
    }
}

public struct ReviewComment: Codable, Sendable, Equatable {
    public var id: String?
    public var worktree: String
    public var path: String
    public var start: Int
    public var end: Int
    public var removed: Bool?
    @NullAsEmpty public var code: [String]
    public var body: String

    public init(id: String? = nil, worktree: String, path: String, start: Int, end: Int, removed: Bool? = nil, code: [String], body: String) {
        self.id = id
        self.worktree = worktree
        self.path = path
        self.start = start
        self.end = end
        self.removed = removed
        self.code = code
        self.body = body
    }
}

public struct ReviewDraft: Codable, Sendable, Equatable {
    public var id: String
    public var session: String
    public var status: String
    @NullAsEmpty public var comments: [ReviewComment]
    public var sentAt: String?
    @NullAsEmpty public var turns: [String]
    public var note: String?

    public init(id: String, session: String, status: String, comments: [ReviewComment], turns: [String] = [], note: String? = nil) {
        self.id = id
        self.session = session
        self.status = status
        self.comments = comments
        self.turns = turns
        self.note = note
    }

    enum CodingKeys: String, CodingKey {
        case id, session, status, comments, sentAt = "sent_at", turns, note
    }
}

public struct Project: Codable, Sendable, Equatable {
    public var root: String
    public var name: String
    public var setup: String

    public init(root: String, name: String, setup: String) {
        self.root = root
        self.name = name
        self.setup = setup
    }

    enum CodingKeys: String, CodingKey {
        case root = "Root", name = "Name", setup = "Setup"
    }
}

public struct ViewState: Codable, Sendable, Equatable {
    public var seq: UInt64
    public var workspaces: [Workspace]
    public var projects: [Project]
    public var tasks: [TaskItem]
    public var worktrees: [Worktree]
    public var sessions: [Session]
    public var limits: [JSONValue]
    public var queue: [JSONValue]
    public var sends: [QueuedSend]
    public var events: [SessionEvent]
    public var subagents: [Subagent]
    public var drafts: [ReviewDraft]
    public var reclaimable: Reclaimable?
}

public struct Reclaimable: Codable, Sendable, Equatable {
    public var size: Int64
    public var pending: Int

    public init(size: Int64, pending: Int) {
        self.size = size
        self.pending = pending
    }
}

public struct ViewDiff: Codable, Sendable, Equatable {
    public var seq: UInt64
    public var removedWorkspace: String?
    public var removedWorktree: String?
    public var removedSession: String?
    public var removedProject: String?
    public var workspace: Workspace?
    public var project: Project?
    public var task: TaskItem?
    public var worktree: Worktree?
    public var session: Session?
    public var limits: [JSONValue]?
    public var queue: [JSONValue]?
    public var sends: [QueuedSend]?
    public var event: SessionEvent?
    public var subagent: Subagent?
    public var draft: ReviewDraft?
    public var comment: ReviewComment?
    public var reclaimable: Reclaimable?

    public init(seq: UInt64) {
        self.seq = seq
    }

    enum CodingKeys: String, CodingKey {
        case seq, removedWorkspace = "removed_workspace", removedWorktree = "removed_worktree"
        case removedSession = "removed_session", workspace, task, worktree, session, limits, queue, sends
        case event, subagent, draft, comment, reclaimable
        case removedProject = "removed_project", project
    }
}
