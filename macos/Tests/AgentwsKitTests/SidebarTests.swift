import Testing
import AgentwsKit

struct SidebarTests {
    @Test func rowsFollowTheDaemonOrderWithShortcutsForTheFirstNine() {
        let sessions = (1...11).map { SeedSession(id: "s\($0)", name: "task \($0)", state: "idle") }
        let sidebar = Sidebar(state: Seed.state(sessions: sessions))
        #expect(sidebar.rows.map(\.id) == (1...11).map { "s\($0)" })
        #expect(sidebar.rows.prefix(9).map(\.shortcut) == (1...9).map { "⌘\($0)" })
        #expect(sidebar.rows[9].shortcut == nil)
    }

    @Test func aRowCarriesNameBadgeWhereAndDetail() {
        let session = SeedSession(
            id: "s1", name: "fix login", state: "waiting", harness: "codex", where: "api@42-retry +1",
            model: "gpt-5", effort: "high", contextLeft: 38, unread: true, muted: true
        )
        let row = Sidebar(state: Seed.state(sessions: [session])).rows[0]
        #expect(row.name == "fix login")
        #expect(row.bold)
        #expect(row.muted)
        #expect(row.badge == "CX")
        #expect(row.where == "api@42-retry +1")
        #expect(row.detail == "gpt-5 · high · ctx 38%")
        #expect(row.style == StateStyle(state: "waiting"))
    }

    @Test func detailLeavesOutWhatIsUnknown() {
        let row = Sidebar(state: Seed.state(sessions: [SeedSession(id: "s1", name: "a", state: "idle", model: "opus")])).rows[0]
        #expect(row.badge == "CC")
        #expect(!row.bold)
        #expect(!row.muted)
        #expect(row.detail == "opus")
    }

    @Test func worktreeRowsShowSubtaskPRChecksAndPorts() {
        let session = SeedSession(id: "s1", name: "a", state: "running", worktrees: [
            SeedWorktree(id: "w1", repo: "api", branch: "42-retry", subtask: "retry", pr: 42, checks: "passing", ports: [3000, 5173]),
            SeedWorktree(id: "w2", repo: "web", branch: "fix", pr: 7, checks: "failing"),
            SeedWorktree(id: "w3", repo: "docs", branch: "x", pr: 9, checks: "pending"),
            SeedWorktree(id: "w4", repo: "cli", branch: "y"),
        ])
        let row = Sidebar(state: Seed.state(sessions: [session])).rows[0]
        #expect(row.worktrees == ["api:retry #42 ✓ :3000 :5173", "web #7 ✗", "docs #9 ●", "cli"])
    }

    @Test func endedSessionsGoToTheEndedGroup() {
        let state = Seed.state(sessions: [
            SeedSession(id: "s1", name: "a", state: "idle"),
            SeedSession(id: "s2", name: "b", state: "done", ended: true),
        ])
        let sidebar = Sidebar(state: state)
        #expect(sidebar.rows.map(\.id) == ["s1"])
        #expect(sidebar.ended.map(\.id) == ["s2"])
        #expect(sidebar.ended[0].shortcut == nil)
    }

    @Test func theFooterCountsSessionsAndWhoNeedsYou() {
        let state = Seed.state(sessions: [
            SeedSession(id: "s1", name: "a", state: "permission"),
            SeedSession(id: "s2", name: "b", state: "waiting"),
            SeedSession(id: "s3", name: "c", state: "running"),
            SeedSession(id: "s4", name: "d", state: "done", ended: true),
        ])
        #expect(Sidebar(state: state).footer == "3 sessions · 2 need you · 1 ended")
        #expect(Sidebar(state: Seed.state(sessions: [SeedSession(id: "s1", name: "a", state: "idle")])).footer == "1 session")
    }

    @Test func theFooterEndsWithTheReclaimableDisk() {
        var state = Seed.state(sessions: [SeedSession(id: "s1", name: "a", state: "idle")])
        state.reclaimable = Reclaimable(size: 1_200_000_000, pending: 0)
        #expect(Sidebar(state: state).footer == "1 session · 1.2 GB reclaimable")
        state.reclaimable = Reclaimable(size: 1_200_000_000, pending: 2)
        #expect(Sidebar(state: state).footer == "1 session · 1.2 GB+ reclaimable")
        state.reclaimable = Reclaimable(size: 0, pending: 0)
        #expect(Sidebar(state: state).footer == "1 session")
    }

    @Test func theFilterMatchesNameAndWhereIgnoringCase() {
        let state = Seed.state(sessions: [
            SeedSession(id: "s1", name: "Fix login", state: "idle", where: "api@feat"),
            SeedSession(id: "s2", name: "docs", state: "idle", where: "web@fix"),
            SeedSession(id: "s3", name: "other", state: "idle", where: "cli@main"),
        ])
        #expect(Sidebar(state: state, filter: "LOGIN").rows.map(\.id) == ["s1"])
        #expect(Sidebar(state: state, filter: "fix").rows.map(\.id) == ["s1", "s2"])
        #expect(Sidebar(state: state, filter: "  ").rows.count == 3)
    }

    @Test func theSeededWindowHasTenSessionsInEveryState() {
        let sidebar = Sidebar(state: Seed.window)
        #expect(sidebar.rows.count + sidebar.ended.count == 10)
        let states = Set((sidebar.rows + sidebar.ended).map(\.style.label))
        #expect(states.isSuperset(of: ["Permission", "Waiting", "Running", "Done", "Idle"]))
        #expect(!sidebar.ended.isEmpty)
    }
}
