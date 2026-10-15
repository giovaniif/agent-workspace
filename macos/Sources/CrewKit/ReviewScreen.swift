import Foundation

public struct Segment: Sendable, Equatable {
    public var text: String
    public var cls: String?

    public init(text: String, cls: String?) {
        self.text = text
        self.cls = cls
    }
}

public enum Highlight {
    public static func segments(_ line: DiffLine) -> [Segment] {
        let bytes = Array(line.text.utf8)
        guard !bytes.isEmpty else { return [] }
        let spans = (line.spans ?? []).sorted { $0.start < $1.start }
        var out: [Segment] = []
        var cursor = 0
        func text(_ from: Int, _ to: Int) -> String { String(decoding: bytes[from..<to], as: UTF8.self) }
        for span in spans {
            let start = max(span.start, cursor)
            let end = min(span.end, bytes.count)
            guard start < end else { continue }
            if start > cursor { out.append(Segment(text: text(cursor, start), cls: nil)) }
            out.append(Segment(text: text(start, end), cls: span.cls))
            cursor = end
        }
        if cursor < bytes.count { out.append(Segment(text: text(cursor, bytes.count), cls: nil)) }
        return out
    }
}

public struct SplitRow: Sendable, Equatable {
    public var left: DiffLine?
    public var right: DiffLine?

    public init(left: DiffLine?, right: DiffLine?) {
        self.left = left
        self.right = right
    }

    public static func rows(_ hunk: DiffHunk) -> [SplitRow] {
        var out: [SplitRow] = []
        var removed: [DiffLine] = []
        var added: [DiffLine] = []
        func flush() {
            for i in 0..<max(removed.count, added.count) {
                out.append(SplitRow(left: i < removed.count ? removed[i] : nil, right: i < added.count ? added[i] : nil))
            }
            removed = []
            added = []
        }
        for line in hunk.lines {
            switch line.kind {
            case .deleted:
                if !added.isEmpty { flush() }
                removed.append(line)
            case .added:
                added.append(line)
            case .context:
                flush()
                out.append(SplitRow(left: line, right: line))
            }
        }
        flush()
        return out
    }
}

public struct CommentAnchor: Sendable, Equatable {
    public var start: Int
    public var end: Int
    public var removed: Bool
    public var code: [String]

    public init(start: Int, end: Int, removed: Bool, code: [String]) {
        self.start = start
        self.end = end
        self.removed = removed
        self.code = code
    }

    public init?(lines: [DiffLine]) {
        guard !lines.isEmpty else { return nil }
        let onlyRemoved = lines.allSatisfy { $0.kind == .deleted }
        removed = onlyRemoved
        code = lines.map(\.text)
        let numbers = lines.map { onlyRemoved ? $0.old : $0.new }.filter { $0 > 0 }
        guard let low = numbers.min(), let high = numbers.max() else { return nil }
        start = low
        end = high
    }
}

public enum DiffLayout: String, Sendable, Equatable, CaseIterable {
    case unified
    case split
}

public struct FileKey: Hashable, Sendable {
    public var worktree: String
    public var path: String

    public init(worktree: String, path: String) {
        self.worktree = worktree
        self.path = path
    }
}

public struct TreeFile: Sendable, Equatable, Identifiable {
    public var key: FileKey
    public var path: String
    public var status: String
    public var added: Int
    public var deleted: Int
    public var viewed: Bool

    public var id: FileKey { key }
}

public struct TreeGroup: Sendable, Equatable, Identifiable {
    public var id: String
    public var title: String
    public var error: String
    public var files: [TreeFile]
}

public struct WorktreeChoice: Sendable, Equatable, Identifiable {
    public var id: String
    public var title: String
}

public struct SelectedFile: Sendable, Equatable {
    public var worktree: ReviewWorktree
    public var file: FileDiff

    public var key: FileKey { FileKey(worktree: worktree.id, path: file.path) }
}

public struct DraftRow: Sendable, Equatable, Identifiable {
    public var id: String
    public var label: String
    public var body: String
}

public struct Composer: Sendable, Equatable {
    public var key: FileKey
    public var lines: [DiffLine]
    public var text: String

    public init(key: FileKey, lines: [DiffLine], text: String = "") {
        self.key = key
        self.lines = lines
        self.text = text
    }
}

public struct ReviewScreen: Sendable, Equatable {
    public var session: String
    public var sessionState: String
    public var scope: ReviewScope
    public var worktree: String
    public var layout: DiffLayout
    public var result: ReviewResult?
    public var viewed: [ViewedMark]
    public var draft: ReviewDraft?
    public var note: String
    public var selected: FileKey?
    public var composer: Composer?
    public var error: String?
    public var sidebarShown: Bool

    public init(
        session: String, sessionState: String = "idle", scope: ReviewScope = .lastTurn, worktree: String = "",
        layout: DiffLayout = .unified
    ) {
        self.session = session
        self.sessionState = sessionState
        self.scope = scope
        self.worktree = worktree
        self.layout = layout
        viewed = []
        note = ""
        sidebarShown = false
    }

    var visible: [WorktreeReview] {
        let all = result?.worktrees ?? []
        return worktree.isEmpty ? all : all.filter { $0.worktree.id == worktree }
    }

    public func isViewed(_ key: FileKey, blob: String) -> Bool {
        viewed.contains { $0.worktree == key.worktree && $0.path == key.path && $0.blob == blob }
    }

    public var tree: [TreeGroup] {
        visible.map { w in
            TreeGroup(id: w.worktree.id, title: w.worktree.title, error: w.err, files: w.files.map { f in
                let key = FileKey(worktree: w.worktree.id, path: f.path)
                return TreeFile(key: key, path: f.path, status: f.statusLetter, added: f.added, deleted: f.deleted, viewed: isViewed(key, blob: f.blob))
            })
        }
    }

    public var worktreeMenu: [WorktreeChoice] {
        [WorktreeChoice(id: "", title: "All")] + (result?.worktrees ?? []).map { WorktreeChoice(id: $0.worktree.id, title: $0.worktree.title) }
    }

    public func file(_ key: FileKey) -> SelectedFile? {
        for w in result?.worktrees ?? [] where w.worktree.id == key.worktree {
            if let f = w.files.first(where: { $0.path == key.path }) { return SelectedFile(worktree: w.worktree, file: f) }
        }
        return nil
    }

    public var selectedFile: SelectedFile? {
        let shown = visible
        if let selected, shown.contains(where: { $0.worktree.id == selected.worktree }), let found = file(selected) {
            return found
        }
        for w in shown {
            if let f = w.files.first { return SelectedFile(worktree: w.worktree, file: f) }
        }
        return nil
    }

    public mutating func startComment(_ key: FileKey, _ line: DiffLine) {
        selected = key
        composer = Composer(key: key, lines: [line])
    }

    public mutating func extendComment(to line: DiffLine) {
        guard let file = selectedFile else { return }
        guard var composer, composer.key == file.key,
              let first = composer.lines.first, let last = composer.lines.last else {
            startComment(file.key, line)
            return
        }
        let all = file.file.hunks.flatMap(\.lines)
        guard let from = all.firstIndex(of: first), let to = all.firstIndex(of: last), let target = all.firstIndex(of: line) else {
            return
        }
        composer.lines = Array(all[min(from, target)...max(to, target)])
        self.composer = composer
    }

    public var fileCount: Int { visible.reduce(0) { $0 + $1.files.count } }

    public var draftRows: [DraftRow] {
        (draft?.comments ?? []).enumerated().map { i, c in
            let named = result?.worktrees.first { $0.worktree.path == c.worktree || $0.worktree.id == c.worktree }
            let name = named.map { ($0.worktree.repo as NSString).lastPathComponent } ?? (c.worktree as NSString).lastPathComponent
            let lines = c.start == c.end ? "\(c.start)" : "\(c.start)-\(c.end)"
            return DraftRow(id: c.id ?? "\(i)", label: "\(name) · \(c.path):\(lines)", body: c.body)
        }
    }

    public var canSend: Bool {
        guard let draft, !draft.comments.isEmpty else { return false }
        return draft.status == "open" || draft.status.isEmpty
    }

    public var queueNotice: String? {
        switch sessionState {
        case "idle", "done", "waiting": nil
        default: "The session is busy; the review will queue until it stops."
        }
    }
}
