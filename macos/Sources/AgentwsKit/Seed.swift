import Foundation

public struct SeedWorktree: Sendable {
    public var id: String
    public var repo: String
    public var branch: String
    public var subtask: String
    public var pr: Int?
    public var checks: String
    public var ports: [Int]

    public init(id: String, repo: String, branch: String, subtask: String = "", pr: Int? = nil, checks: String = "", ports: [Int] = []) {
        self.id = id
        self.repo = repo
        self.branch = branch
        self.subtask = subtask
        self.pr = pr
        self.checks = checks
        self.ports = ports
    }
}

public struct SeedSession: Sendable {
    public var id: String
    public var name: String
    public var state: String
    public var harness: String
    public var `where`: String
    public var model: String
    public var effort: String
    public var contextLeft: Int?
    public var unread: Bool
    public var muted: Bool
    public var ended: Bool
    public var since: String?
    public var worktrees: [SeedWorktree]

    public init(
        id: String, name: String, state: String, harness: String = "claude", where: String = "",
        model: String = "", effort: String = "", contextLeft: Int? = nil, unread: Bool = false,
        muted: Bool = false, ended: Bool = false, since: String? = nil, worktrees: [SeedWorktree] = []
    ) {
        self.id = id
        self.name = name
        self.state = state
        self.harness = harness
        self.where = `where`
        self.model = model
        self.effort = effort
        self.contextLeft = contextLeft
        self.unread = unread
        self.muted = muted
        self.ended = ended
        self.since = since
        self.worktrees = worktrees
    }
}

public struct SeedQuota: Sendable {
    public var harness: String
    public var label: String
    public var left: Int
    public var resetsAt: Date
    public var staleAt: Date

    public init(harness: String, label: String, left: Int, resetsAt: Date, staleAt: Date) {
        self.harness = harness
        self.label = label
        self.left = left
        self.resetsAt = resetsAt
        self.staleAt = staleAt
    }
}

public enum Seed {
    public static let now = Date(timeIntervalSince1970: 1_800_000_000)

    public static func state(sessions: [SeedSession], limits: [SeedQuota] = []) -> ViewState {
        let stamp = ISO8601DateFormatter()
        var order = 0
        var worktrees: [[String: Any]] = []
        var out: [[String: Any]] = []
        for s in sessions {
            var board: [[String: Any]] = []
            for w in s.worktrees {
                var pr: Any = NSNull()
                if let number = w.pr {
                    pr = [
                        "Number": number, "Title": s.name, "URL": "https://example.com/pull/\(number)", "Head": w.branch,
                        "State": "OPEN", "Checks": w.checks, "ReviewDecision": "", "Mergeable": "MERGEABLE",
                        "UnresolvedThreads": 0, "BotComments": 0, "Failing": NSNull(),
                    ] as [String: Any]
                    board.append([
                        "number": number, "title": s.name, "url": "https://example.com/pull/\(number)", "branch": w.branch,
                        "state": "OPEN", "checks": w.checks, "failing_checks": [] as [Any], "review_decision": "",
                        "unresolved_threads": 0, "bot_comments_since_push": 0, "mergeable": "MERGEABLE",
                        "ready_to_merge": w.checks == "passing", "blockers": [] as [Any],
                    ])
                }
                worktrees.append([
                    "ID": w.id, "Repo": w.repo, "Path": "/w/\(w.repo)-\(w.branch)", "Branch": w.branch, "PR": pr,
                    "SubtaskSlug": w.subtask, "SessionID": s.id,
                    "Ports": w.ports.isEmpty ? NSNull() : w.ports.map { ["Port": $0, "PID": 1, "PGID": 1, "Command": "node"] },
                ])
            }
            if !s.ended { order += 1 }
            out.append([
                "ID": s.id, "TaskID": "t-" + s.id, "Harness": s.harness, "Pane": "%\(order)", "Model": s.model,
                "Effort": s.effort, "State": s.state, "Ended": s.ended, "Unread": s.unread, "Focused": false,
                "Muted": s.muted, "WorktreeIDs": s.worktrees.map(\.id), "ResumeID": "", "Transcript": "", "Dir": "",
                "Usage": ["ContextLeftPercent": s.contextLeft ?? 0, "HasContext": s.contextLeft != nil, "LimitUsedPercent": 0],
                "Limits": NSNull(), "LimitsAt": "0001-01-01T00:00:00Z", "Switches": NSNull(), "SwitchWarning": false,
                "name": s.name, "where": s.where, "banner": "", "since": s.since ?? NSNull(),
                "order": s.ended ? -1 : order, "board": board,
            ])
        }
        let quotas: [[String: Any]] = limits.map { q in
            [
                "Harness": q.harness, "Window": q.label, "LeftPercent": q.left,
                "ResetsAt": Int(q.resetsAt.timeIntervalSince1970), "ReportedAt": stamp.string(from: q.staleAt),
                "label": q.label, "low": q.left < 20, "stale_at": stamp.string(from: q.staleAt),
            ]
        }
        let object: [String: Any] = [
            "seq": 1, "workspaces": [] as [Any], "tasks": [] as [Any], "worktrees": worktrees, "sessions": out,
            "limits": quotas, "queue": [] as [Any], "sends": [] as [Any], "events": [] as [Any],
            "subagents": [] as [Any], "drafts": [] as [Any],
        ]
        do {
            return try JSONDecoder().decode(ViewState.self, from: JSONSerialization.data(withJSONObject: object))
        } catch {
            preconditionFailure("seed state does not decode: \(error)")
        }
    }

    public static var window: ViewState {
        let ago = { (seconds: Double) in ISO8601DateFormatter().string(from: now.addingTimeInterval(-seconds)) }
        return state(sessions: [
            SeedSession(id: "s1", name: "fix login redirect", state: "permission", where: "api@42-login-redirect",
                        model: "opus", effort: "high", contextLeft: 38, unread: true, since: ago(95),
                        worktrees: [SeedWorktree(id: "w1", repo: "api", branch: "42-login-redirect", pr: 42, checks: "pending", ports: [3000])]),
            SeedSession(id: "s2", name: "add billing export", state: "waiting", harness: "codex", where: "web@billing-export +1",
                        model: "gpt-5", effort: "medium", contextLeft: 71, unread: true, since: ago(260),
                        worktrees: [
                            SeedWorktree(id: "w2", repo: "web", branch: "billing-export", pr: 118, checks: "passing", ports: [5173]),
                            SeedWorktree(id: "w3", repo: "api", branch: "billing-export", subtask: "export", pr: 43, checks: "failing"),
                        ]),
            SeedSession(id: "s3", name: "quickstart docs", state: "waiting", where: "docs@quickstart",
                        model: "sonnet", effort: "low", contextLeft: 90, since: ago(1_020),
                        worktrees: [SeedWorktree(id: "w4", repo: "docs", branch: "quickstart")]),
            SeedSession(id: "s4", name: "refactor cache layer", state: "running", where: "api@cache-layer",
                        model: "opus", effort: "high", contextLeft: 54, since: ago(40),
                        worktrees: [SeedWorktree(id: "w5", repo: "api", branch: "cache-layer")]),
            SeedSession(id: "s5", name: "deflake e2e", state: "running", harness: "codex", where: "web@e2e-flake",
                        model: "gpt-5", effort: "high", contextLeft: 22, since: ago(610),
                        worktrees: [SeedWorktree(id: "w6", repo: "web", branch: "e2e-flake", pr: 120, checks: "pending")]),
            SeedSession(id: "s6", name: "bump dependencies", state: "done", where: "cli@bump-deps",
                        model: "sonnet", effort: "medium", contextLeft: 66, unread: true, since: ago(1_800),
                        worktrees: [SeedWorktree(id: "w7", repo: "cli", branch: "bump-deps", pr: 77, checks: "passing")]),
            SeedSession(id: "s7", name: "search index", state: "done", harness: "codex", where: "api@search-index",
                        model: "gpt-5", effort: "low", contextLeft: 47, muted: true, since: ago(5_400),
                        worktrees: [SeedWorktree(id: "w8", repo: "api", branch: "search-index")]),
            SeedSession(id: "s8", name: "spike: tmux control mode", state: "idle", where: "agentws@spike",
                        model: "opus", effort: "medium", contextLeft: 81, since: ago(9_000)),
            SeedSession(id: "s9", name: "release notes", state: "idle", where: "docs@release-notes",
                        model: "haiku", muted: true),
            SeedSession(id: "s10", name: "old migration", state: "done", where: "api@migration", model: "opus", ended: true),
        ], limits: [
            SeedQuota(harness: "claude", label: "5h", left: 64, resetsAt: now.addingTimeInterval(7_200), staleAt: now.addingTimeInterval(600)),
            SeedQuota(harness: "claude", label: "7d", left: 41, resetsAt: now.addingTimeInterval(259_200), staleAt: now.addingTimeInterval(600)),
            SeedQuota(harness: "codex", label: "7d", left: 14, resetsAt: now.addingTimeInterval(86_400), staleAt: now.addingTimeInterval(-60)),
        ])
    }
}
