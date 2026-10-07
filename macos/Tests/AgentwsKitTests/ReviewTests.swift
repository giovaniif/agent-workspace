import Foundation
import Testing
@testable import AgentwsKit

enum ReviewGoldens {
    static let root = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()

    static func tokens() throws -> ReviewResult {
        let data = try Data(contentsOf: root.appendingPathComponent("cmd/agentws/testdata/review_tokens.json.golden"))
        return try JSONDecoder().decode(ReviewReply.self, from: data).result
    }

    struct ReviewReply: Decodable {
        var result: ReviewResult
    }
}

func line(_ kind: LineKind, old: Int = 0, new: Int = 0, _ text: String) -> DiffLine {
    DiffLine(kind: kind, old: old, new: new, text: text)
}

func fileDiff(_ path: String, blob: String = "b1", hunks: [DiffHunk] = []) -> FileDiff {
    FileDiff(path: path, status: "M", added: 1, deleted: 0, blob: blob, hunks: hunks)
}

func fiftyFileReply() -> String {
    var files: [String] = []
    for f in 0..<50 {
        var lines: [String] = []
        for n in 1...60 {
            lines.append(#"{"Kind":43,"Old":0,"New":\#(n),"Text":"\tvalue\#(n) := compute(\"x\", \#(n)) // note","Spans":[[1,7,"name"],[8,10,"operator"],[11,18,"function"],[19,22,"string"],[24,26,"number"],[28,35,"comment"]]}"#)
        }
        let hunk = #"{"Header":"@@ -0,0 +1,60 @@","Lines":["# + lines.joined(separator: ",") + "]}"
        files.append(#"{"Path":"pkg/file\#(f).go","OldPath":"","Status":"A","Added":60,"Deleted":0,"Binary":false,"Blob":"b\#(f)","Hunks":["# + hunk + "]}")
    }
    let worktree = #"{"Worktree":{"ID":"/w/api","Repo":"api","Path":"/w/api","Branch":"feat","PR":null},"From":"main","Files":["# + files.joined(separator: ",") + #"],"Err":""}"#
    return #"{"scope":"branch","worktrees":["# + worktree + #"],"viewed":[],"draft":{"id":"","session":"","status":"","comments":null,"turns":null}}"#
}

struct ReviewTests {
    @Test func decodesTheTokensGolden() throws {
        let review = try ReviewGoldens.tokens()
        #expect(review.scope == .branch)
        #expect(review.worktrees.map(\.worktree.id) == ["w1"])
        #expect(review.worktrees[0].worktree.repo == "api")
        #expect(review.worktrees[0].from == "main")
        let files = review.worktrees[0].files
        #expect(files.map(\.path) == ["main.go", "web/app.ts", "README.md", "notes.unknownext"])
        let first = try #require(files[0].hunks.first?.lines.first)
        #expect(first.kind == .added)
        #expect(first.spans == [TokenSpan(start: 0, end: 7, cls: "keyword")])
        #expect(review.draft?.comments.isEmpty ?? true)
    }

    @Test func goKindsDecodeFromTheirByteValues() throws {
        let json = #"[{"Kind":32,"Old":1,"New":1,"Text":"a"},{"Kind":43,"Old":0,"New":2,"Text":"b"},{"Kind":45,"Old":2,"New":0,"Text":"c","NoEOL":true}]"#
        let lines = try JSONDecoder().decode([DiffLine].self, from: Data(json.utf8))
        #expect(lines.map(\.kind) == [.context, .added, .deleted])
        #expect(lines[2].noEOL)
        let back = try JSONSerialization.jsonObject(with: JSONEncoder().encode(lines)) as? [[String: Any]]
        #expect(back?.map { $0["Kind"] as? Int } == [32, 43, 45])
    }

    @Test func highlightSplitsALineByItsByteSpans() throws {
        let review = try ReviewGoldens.tokens()
        let lines = review.worktrees[0].files[0].hunks[0].lines
        let segments = Highlight.segments(lines[0])
        #expect(segments == [Segment(text: "package", cls: "keyword"), Segment(text: " main", cls: nil)])
        #expect(Highlight.segments(lines[1]) == [])
        #expect(Highlight.segments(lines[3]).map(\.text).joined() == lines[3].text)
    }

    @Test func highlightCountsBytesNotCharacters() {
        var l = line(.added, new: 1, "é := 1")
        l.spans = [TokenSpan(start: 3, end: 5, cls: "operator")]
        #expect(Highlight.segments(l) == [
            Segment(text: "é ", cls: nil), Segment(text: ":=", cls: "operator"), Segment(text: " 1", cls: nil),
        ])
    }

    @Test func highlightIgnoresSpansOutsideTheLine() {
        var l = line(.added, new: 1, "ab")
        l.spans = [TokenSpan(start: 1, end: 99, cls: "string"), TokenSpan(start: 5, end: 6, cls: "number")]
        #expect(Highlight.segments(l) == [Segment(text: "a", cls: nil), Segment(text: "b", cls: "string")])
        #expect(Highlight.segments(line(.context, "plain")) == [Segment(text: "plain", cls: nil)])
    }

    @Test func splitRowsPairRemovedLinesWithTheAddedOnesAfterThem() {
        let c1 = line(.context, old: 1, new: 1, "a")
        let d1 = line(.deleted, old: 2, "b")
        let d2 = line(.deleted, old: 3, "c")
        let a1 = line(.added, new: 2, "B")
        let c2 = line(.context, old: 4, new: 3, "d")
        let a2 = line(.added, new: 4, "e")
        let rows = SplitRow.rows(DiffHunk(header: "@@", lines: [c1, d1, d2, a1, c2, a2]))
        #expect(rows == [
            SplitRow(left: c1, right: c1), SplitRow(left: d1, right: a1), SplitRow(left: d2, right: nil),
            SplitRow(left: c2, right: c2), SplitRow(left: nil, right: a2),
        ])
    }

    @Test func aCommentAnchorsToTheNewSideUnlessEveryLineWasRemoved() throws {
        let mixed = try #require(CommentAnchor(lines: [
            line(.deleted, old: 7, "a"), line(.context, old: 8, new: 7, "b"), line(.added, new: 8, "c"),
        ]))
        #expect(mixed == CommentAnchor(start: 7, end: 8, removed: false, code: ["a", "b", "c"]))
        let removed = try #require(CommentAnchor(lines: [line(.deleted, old: 3, "x"), line(.deleted, old: 4, "y")]))
        #expect(removed == CommentAnchor(start: 3, end: 4, removed: true, code: ["x", "y"]))
        #expect(CommentAnchor(lines: []) == nil)
    }

    @Test func aShiftClickExtendsTheCommentToTheLinesBetween() {
        let lines = (1...5).map { line(.added, new: $0, "l\($0)") }
        let key = FileKey(worktree: "/w/api", path: "a.go")
        var screen = ReviewScreen(session: "s1")
        screen.result = ReviewResult(scope: .branch, worktrees: [
            WorktreeReview(worktree: ReviewWorktree(id: "/w/api", repo: "api", path: "/w/api", branch: "x"), from: "main",
                           files: [fileDiff("a.go", hunks: [DiffHunk(header: "@@", lines: Array(lines[0..<2])), DiffHunk(header: "@@", lines: Array(lines[2...]))])]),
        ])
        screen.startComment(key, lines[3])
        screen.composer?.text = "draft"
        screen.extendComment(to: lines[1])
        #expect(screen.composer?.lines == Array(lines[1...3]))
        #expect(screen.composer?.text == "draft")
        screen.extendComment(to: lines[4])
        #expect(screen.composer?.lines == Array(lines[1...4]))
        screen.startComment(key, lines[0])
        #expect(screen.composer == Composer(key: key, lines: [lines[0]]))
        screen.composer = nil
        screen.extendComment(to: lines[2])
        #expect(screen.composer == Composer(key: key, lines: [lines[2]]))
    }

    @Test func theTreeGroupsFilesByWorktreeWithItsPR() {
        let api = ReviewWorktree(id: "/w/api", repo: "api", path: "/w/api", branch: "42-login", pr: 42)
        let web = ReviewWorktree(id: "/w/web", repo: "web", path: "/w/web", branch: "billing")
        var screen = ReviewScreen(session: "s1")
        screen.result = ReviewResult(scope: .branch, worktrees: [
            WorktreeReview(worktree: api, from: "main", files: [fileDiff("a.go", blob: "x1"), fileDiff("b.go", blob: "x2")]),
            WorktreeReview(worktree: web, from: "main", files: [fileDiff("app.ts")]),
        ])
        screen.viewed = [ViewedMark(worktree: "/w/api", path: "a.go", blob: "x1"), ViewedMark(worktree: "/w/api", path: "b.go", blob: "old")]
        let tree = screen.tree
        #expect(tree.map(\.title) == ["api #42", "web@billing"])
        #expect(tree[0].files.map(\.path) == ["a.go", "b.go"])
        #expect(tree[0].files.map(\.viewed) == [true, false])
        #expect(screen.worktreeMenu.map(\.title) == ["All", "api #42", "web@billing"])
        #expect(screen.worktreeMenu.map(\.id) == ["", "/w/api", "/w/web"])
        #expect(screen.selectedFile?.file.path == "a.go")
        screen.worktree = "/w/web"
        #expect(screen.tree.map(\.title) == ["web@billing"])
        #expect(screen.selectedFile?.file.path == "app.ts")
    }

    @Test func aWorktreeTheDaemonCouldNotDiffShowsItsError() {
        var screen = ReviewScreen(session: "s1")
        screen.result = ReviewResult(scope: .uncommitted, worktrees: [
            WorktreeReview(worktree: ReviewWorktree(id: "/w/api", repo: "api", path: "/w/api", branch: "x"), from: "", files: [], err: "not a git repository"),
        ])
        #expect(screen.tree.map(\.error) == ["not a git repository"])
    }

    @Test func draftRowsNameTheWorktreeFileAndLines() {
        var screen = ReviewScreen(session: "s1")
        screen.result = ReviewResult(scope: .branch, worktrees: [
            WorktreeReview(worktree: ReviewWorktree(id: "/w/api-feat", repo: "api", path: "/w/api-feat", branch: "feat"), from: "main", files: []),
        ])
        screen.draft = ReviewDraft(id: "d1", session: "s1", status: "open", comments: [
            ReviewComment(id: "c1", worktree: "/w/api-feat", path: "main.go", start: 3, end: 3, code: ["x"], body: "rename"),
            ReviewComment(id: "c2", worktree: "/w/other", path: "b.go", start: 4, end: 6, code: [], body: "split"),
        ])
        #expect(screen.draftRows.map(\.label) == ["api · main.go:3", "other · b.go:4-6"])
        #expect(screen.draftRows.map(\.body) == ["rename", "split"])
        #expect(screen.canSend)
    }

    @Test func sendingToABusySessionSaysTheReviewWillQueue() {
        var screen = ReviewScreen(session: "s1")
        #expect(!screen.canSend)
        screen.draft = ReviewDraft(id: "d1", session: "s1", status: "open", comments: [
            ReviewComment(worktree: "/w", path: "a", start: 1, end: 1, code: [], body: "x"),
        ])
        for state in ["idle", "done", "waiting"] {
            screen.sessionState = state
            #expect(screen.queueNotice == nil, "\(state)")
        }
        for state in ["running", "permission"] {
            screen.sessionState = state
            #expect(screen.queueNotice == "The session is busy; the review will queue until it stops.", "\(state)")
        }
    }

    @Test func scopesStepWithBracketsAndWrap() {
        #expect(ReviewScope.allCases.map(\.title) == ["Last turn", "Uncommitted", "Branch vs main"])
        #expect(ReviewScope.lastTurn.step(1) == .uncommitted)
        #expect(ReviewScope.branch.step(1) == .lastTurn)
        #expect(ReviewScope.lastTurn.step(-1) == .branch)
    }

    @Test func openingAFiftyFileReviewIsUnderItsBudget() throws {
        let data = Data(fiftyFileReply().utf8)
        let clock = ContinuousClock()
        var drawn = 0
        let elapsed = try clock.measure {
            let result = try JSONDecoder().decode(ReviewResult.self, from: data)
            var screen = ReviewScreen(session: "s1")
            screen.result = result
            drawn = screen.tree.flatMap(\.files).count
            if let selected = screen.selectedFile {
                for hunk in selected.file.hunks {
                    for l in hunk.lines { drawn += Highlight.segments(l).count }
                }
            }
        }
        #expect(drawn > 50)
        #expect(elapsed < .milliseconds(300))
    }
}
