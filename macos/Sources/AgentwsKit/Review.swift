import Foundation

public enum ReviewScope: String, Codable, Sendable, Equatable, CaseIterable {
    case lastTurn = "last_turn"
    case uncommitted
    case branch

    public var title: String {
        switch self {
        case .lastTurn: "Last turn"
        case .uncommitted: "Uncommitted"
        case .branch: "Branch vs main"
        }
    }

    public func step(_ by: Int) -> ReviewScope {
        let all = Self.allCases
        let index = all.firstIndex(of: self) ?? 0
        let count = all.count
        return all[((index + by) % count + count) % count]
    }
}

public enum LineKind: Sendable, Equatable {
    case context
    case added
    case deleted

    var byte: Int {
        switch self {
        case .context: 32
        case .added: 43
        case .deleted: 45
        }
    }

    init(byte: Int) {
        switch byte {
        case 43: self = .added
        case 45: self = .deleted
        default: self = .context
        }
    }
}

public struct TokenSpan: Codable, Sendable, Equatable {
    public var start: Int
    public var end: Int
    public var cls: String

    public init(start: Int, end: Int, cls: String) {
        self.start = start
        self.end = end
        self.cls = cls
    }

    public init(from decoder: Decoder) throws {
        var c = try decoder.unkeyedContainer()
        start = try c.decode(Int.self)
        end = try c.decode(Int.self)
        cls = try c.decode(String.self)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.unkeyedContainer()
        try c.encode(start)
        try c.encode(end)
        try c.encode(cls)
    }
}

public struct DiffLine: Codable, Sendable, Equatable {
    public var kind: LineKind
    public var old: Int
    public var new: Int
    public var text: String
    public var noEOL: Bool
    public var spans: [TokenSpan]?

    public init(kind: LineKind, old: Int = 0, new: Int = 0, text: String, noEOL: Bool = false, spans: [TokenSpan]? = nil) {
        self.kind = kind
        self.old = old
        self.new = new
        self.text = text
        self.noEOL = noEOL
        self.spans = spans
    }

    enum CodingKeys: String, CodingKey {
        case kind = "Kind", old = "Old", new = "New", text = "Text", noEOL = "NoEOL", spans = "Spans"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = LineKind(byte: try c.decode(Int.self, forKey: .kind))
        old = try c.decodeIfPresent(Int.self, forKey: .old) ?? 0
        new = try c.decodeIfPresent(Int.self, forKey: .new) ?? 0
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
        noEOL = try c.decodeIfPresent(Bool.self, forKey: .noEOL) ?? false
        spans = try c.decodeIfPresent([TokenSpan].self, forKey: .spans)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(kind.byte, forKey: .kind)
        try c.encode(old, forKey: .old)
        try c.encode(new, forKey: .new)
        try c.encode(text, forKey: .text)
        if noEOL { try c.encode(true, forKey: .noEOL) }
    }
}

public struct DiffHunk: Codable, Sendable, Equatable {
    public var header: String
    public var lines: [DiffLine]

    public init(header: String, lines: [DiffLine]) {
        self.header = header
        self.lines = lines
    }

    enum CodingKeys: String, CodingKey {
        case header = "Header", lines = "Lines"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        header = try c.decodeIfPresent(String.self, forKey: .header) ?? ""
        lines = try c.decodeIfPresent([DiffLine].self, forKey: .lines) ?? []
    }
}

public struct FileDiff: Codable, Sendable, Equatable {
    public var path: String
    public var oldPath: String
    public var status: String
    public var added: Int
    public var deleted: Int
    public var binary: Bool
    public var blob: String
    public var mode: String
    public var hunks: [DiffHunk]

    public init(
        path: String, oldPath: String = "", status: String, added: Int = 0, deleted: Int = 0, binary: Bool = false,
        blob: String = "", mode: String = "", hunks: [DiffHunk] = []
    ) {
        self.path = path
        self.oldPath = oldPath
        self.status = status
        self.added = added
        self.deleted = deleted
        self.binary = binary
        self.blob = blob
        self.mode = mode
        self.hunks = hunks
    }

    enum CodingKeys: String, CodingKey {
        case path = "Path", oldPath = "OldPath", status = "Status", added = "Added", deleted = "Deleted"
        case binary = "Binary", blob = "Blob", mode = "Mode", hunks = "Hunks"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.decode(String.self, forKey: .path)
        oldPath = try c.decodeIfPresent(String.self, forKey: .oldPath) ?? ""
        status = try c.decodeIfPresent(String.self, forKey: .status) ?? ""
        added = try c.decodeIfPresent(Int.self, forKey: .added) ?? 0
        deleted = try c.decodeIfPresent(Int.self, forKey: .deleted) ?? 0
        binary = try c.decodeIfPresent(Bool.self, forKey: .binary) ?? false
        blob = try c.decodeIfPresent(String.self, forKey: .blob) ?? ""
        mode = try c.decodeIfPresent(String.self, forKey: .mode) ?? ""
        hunks = try c.decodeIfPresent([DiffHunk].self, forKey: .hunks) ?? []
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(path, forKey: .path)
        try c.encode(oldPath, forKey: .oldPath)
        try c.encode(status, forKey: .status)
        try c.encode(added, forKey: .added)
        try c.encode(deleted, forKey: .deleted)
        try c.encode(binary, forKey: .binary)
        try c.encode(blob, forKey: .blob)
        if !mode.isEmpty { try c.encode(mode, forKey: .mode) }
        try c.encode(hunks, forKey: .hunks)
    }

    public var statusLetter: String {
        switch status {
        case "A", "added": "A"
        case "D", "deleted": "D"
        case "R", "renamed": "R"
        default: "M"
        }
    }
}

public struct ReviewWorktree: Decodable, Sendable, Equatable {
    public var id: String
    public var repo: String
    public var path: String
    public var branch: String
    public var pr: Int?

    public init(id: String, repo: String, path: String, branch: String, pr: Int? = nil) {
        self.id = id
        self.repo = repo
        self.path = path
        self.branch = branch
        self.pr = pr
    }

    enum CodingKeys: String, CodingKey {
        case id = "ID", repo = "Repo", path = "Path", branch = "Branch", pr = "PR"
    }

    struct PRNumber: Decodable {
        var number: Int

        enum CodingKeys: String, CodingKey {
            case number = "Number"
        }
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        repo = try c.decodeIfPresent(String.self, forKey: .repo) ?? ""
        path = try c.decodeIfPresent(String.self, forKey: .path) ?? id
        branch = try c.decodeIfPresent(String.self, forKey: .branch) ?? ""
        pr = try c.decodeIfPresent(PRNumber.self, forKey: .pr)?.number
    }

    public var title: String {
        let name = repo.isEmpty ? (path as NSString).lastPathComponent : (repo as NSString).lastPathComponent
        if let pr { return "\(name) #\(pr)" }
        return branch.isEmpty ? name : "\(name)@\(branch)"
    }
}

public struct WorktreeReview: Decodable, Sendable, Equatable {
    public var worktree: ReviewWorktree
    public var from: String
    public var files: [FileDiff]
    public var err: String

    public init(worktree: ReviewWorktree, from: String, files: [FileDiff], err: String = "") {
        self.worktree = worktree
        self.from = from
        self.files = files
        self.err = err
    }

    enum CodingKeys: String, CodingKey {
        case worktree = "Worktree", from = "From", files = "Files", err = "Err"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        worktree = try c.decode(ReviewWorktree.self, forKey: .worktree)
        from = try c.decodeIfPresent(String.self, forKey: .from) ?? ""
        files = try c.decodeIfPresent([FileDiff].self, forKey: .files) ?? []
        err = try c.decodeIfPresent(String.self, forKey: .err) ?? ""
    }
}

public struct ViewedMark: Codable, Sendable, Equatable {
    public var worktree: String
    public var path: String
    public var blob: String

    public init(worktree: String, path: String, blob: String) {
        self.worktree = worktree
        self.path = path
        self.blob = blob
    }

    enum CodingKeys: String, CodingKey {
        case worktree = "Worktree", path = "Path", blob = "Blob"
    }
}

public struct ReviewResult: Decodable, Sendable, Equatable {
    public var scope: ReviewScope
    public var worktrees: [WorktreeReview]
    public var viewed: [ViewedMark]
    public var draft: ReviewDraft?

    public init(scope: ReviewScope, worktrees: [WorktreeReview], viewed: [ViewedMark] = [], draft: ReviewDraft? = nil) {
        self.scope = scope
        self.worktrees = worktrees
        self.viewed = viewed
        self.draft = draft
    }

    enum CodingKeys: String, CodingKey {
        case scope, worktrees, viewed, draft
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        scope = try c.decode(ReviewScope.self, forKey: .scope)
        worktrees = try c.decodeIfPresent([WorktreeReview].self, forKey: .worktrees) ?? []
        viewed = try c.decodeIfPresent([ViewedMark].self, forKey: .viewed) ?? []
        draft = try? c.decodeIfPresent(ReviewDraft.self, forKey: .draft)
    }
}

public enum HunkAction: String, Codable, Sendable {
    case stage
    case revert
}

struct ReviewOpenParams: Encodable, Sendable {
    var session: String
    var scope: ReviewScope
    var worktree: String
    var tokens = true
}

struct ReviewViewedParams: Encodable, Sendable {
    var mark: ViewedMark
    var viewed: Bool
}

struct ReviewCommentParams: Encodable, Sendable {
    var session: String
    var worktree: String
    var path: String
    var startLine: Int
    var endLine: Int
    var code: String
    var body: String
    var removed: Bool

    enum CodingKeys: String, CodingKey {
        case session, worktree, path, startLine = "start_line", endLine = "end_line", code, body, removed
    }
}

struct ReviewSendParams: Encodable, Sendable {
    var session: String
    var note: String
}

struct ReviewHunkParams: Encodable, Sendable {
    var session: String
    var worktree: String
    var file: FileDiff
    var hunk: Int
    var action: HunkAction
}

struct NvimOpenParams: Encodable, Sendable {
    var session: String
    var worktree: String
    var path: String
    var line: Int
}

struct Empty: Decodable, Sendable {}

struct NvimOpened: Decodable, Sendable {
    var pane: String
    var shown: Bool
}
