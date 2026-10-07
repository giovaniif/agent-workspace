import Foundation

extension Seed {
    static func spanned(_ kind: LineKind, old: Int = 0, new: Int = 0, _ text: String, _ spans: [(Int, Int, String)] = []) -> DiffLine {
        DiffLine(kind: kind, old: old, new: new, text: text, spans: spans.map { TokenSpan(start: $0.0, end: $0.1, cls: $0.2) })
    }

    public static func review(session: String = "s2", layout: DiffLayout = .unified) -> ReviewScreen {
        let web = ReviewWorktree(id: "w2", repo: "web", path: "/w/web-billing-export", branch: "billing-export", pr: 118)
        let api = ReviewWorktree(id: "w3", repo: "api", path: "/w/api-billing-export", branch: "billing-export", pr: 43)
        let exportHunk = DiffHunk(header: "@@ -10,7 +10,9 @@ export function ExportButton()", lines: [
            spanned(.context, old: 10, new: 10, "export function ExportButton() {", [(0, 6, "keyword"), (7, 15, "keyword"), (16, 28, "function")]),
            spanned(.context, old: 11, new: 11, "  const [busy, setBusy] = useState(false);", [(2, 7, "keyword"), (27, 35, "function"), (36, 41, "keyword")]),
            spanned(.deleted, old: 12, "  const url = `/api/export`;", [(2, 7, "keyword"), (14, 27, "string")]),
            spanned(.added, new: 12, "  const url = `/api/billing/export?format=${format}`;", [(2, 7, "keyword"), (14, 52, "string")]),
            spanned(.added, new: 13, "  const format = props.format ?? \"csv\";", [(2, 7, "keyword"), (32, 37, "string")]),
            spanned(.context, old: 13, new: 14, "  async function run() {", [(2, 7, "keyword"), (8, 16, "keyword"), (17, 20, "function")]),
            spanned(.deleted, old: 14, "    await fetch(url);", [(4, 9, "keyword"), (10, 15, "function")]),
            spanned(.added, new: 15, "    setBusy(true);", [(4, 11, "function"), (12, 16, "keyword")]),
            spanned(.added, new: 16, "    await download(url, `billing.${format}`);", [(4, 9, "keyword"), (10, 18, "function"), (24, 43, "string")]),
            spanned(.context, old: 15, new: 17, "  }", []),
        ])
        let testHunk = DiffHunk(header: "@@ -0,0 +1,6 @@", lines: [
            spanned(.added, new: 1, "import { render } from \"@testing-library/react\";", [(0, 6, "keyword"), (18, 22, "keyword"), (23, 47, "string")]),
            spanned(.added, new: 2, "", []),
            spanned(.added, new: 3, "test(\"exports csv by default\", async () => {", [(0, 4, "function"), (5, 29, "string"), (31, 36, "keyword")]),
            spanned(.added, new: 4, "  // clicks the button and waits for the file", [(2, 46, "comment")]),
            spanned(.added, new: 5, "  expect(await exportOnce()).toBe(200);", [(2, 8, "function"), (9, 14, "keyword"), (33, 37, "function"), (38, 41, "number")]),
            spanned(.added, new: 6, "});", []),
        ])
        let handler = DiffHunk(header: "@@ -40,6 +40,8 @@ func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {", lines: [
            spanned(.context, old: 40, new: 40, "\tformat := r.URL.Query().Get(\"format\")", [(8, 10, "operator"), (30, 38, "string")]),
            spanned(.deleted, old: 41, "\trows, err := h.store.All()", [(11, 13, "operator"), (22, 25, "function")]),
            spanned(.added, new: 41, "\trows, err := h.store.Billing(r.Context())", [(11, 13, "operator"), (22, 29, "function")]),
            spanned(.added, new: 42, "\tif format == \"\" {", [(1, 3, "keyword"), (11, 13, "operator"), (14, 16, "string")]),
            spanned(.added, new: 43, "\t\tformat = \"csv\"", [(9, 10, "operator"), (11, 16, "string")]),
            spanned(.added, new: 44, "\t}", []),
        ])
        var screen = ReviewScreen(session: session, sessionState: "waiting", scope: .branch, layout: layout)
        screen.result = ReviewResult(scope: .branch, worktrees: [
            WorktreeReview(worktree: web, from: "main", files: [
                FileDiff(path: "src/billing/ExportButton.tsx", status: "M", added: 4, deleted: 2, blob: "a1", hunks: [exportHunk]),
                FileDiff(path: "src/billing/ExportButton.test.tsx", status: "A", added: 6, blob: "a2", hunks: [testHunk]),
                FileDiff(path: "src/billing/index.ts", status: "M", added: 1, deleted: 0, blob: "a3"),
            ]),
            WorktreeReview(worktree: api, from: "main", files: [
                FileDiff(path: "internal/billing/handler.go", status: "M", added: 4, deleted: 1, blob: "b1", hunks: [handler]),
                FileDiff(path: "internal/billing/store.go", status: "M", added: 12, deleted: 3, blob: "b2"),
            ]),
        ])
        screen.viewed = [ViewedMark(worktree: "w2", path: "src/billing/index.ts", blob: "a3")]
        screen.selected = FileKey(worktree: "w2", path: "src/billing/ExportButton.tsx")
        screen.draft = ReviewDraft(id: "d1", session: session, status: "open", comments: [
            ReviewComment(id: "c1", worktree: "/w/web-billing-export", path: "src/billing/ExportButton.tsx", start: 12, end: 13,
                          code: [], body: "Define format before the url uses it."),
            ReviewComment(id: "c2", worktree: "/w/api-billing-export", path: "internal/billing/handler.go", start: 42, end: 44,
                          code: [], body: "Default the format in one place, not in both apps."),
        ])
        screen.composer = Composer(
            key: FileKey(worktree: "w2", path: "src/billing/ExportButton.tsx"),
            lines: [exportHunk.lines[7], exportHunk.lines[8]],
            text: "Reset busy when the download fails."
        )
        screen.note = "Looks close. Add a test for the json format too."
        return screen
    }
}
