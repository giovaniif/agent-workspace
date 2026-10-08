import Foundation
import Testing
import AgentwsKit

struct ChromeTests {
    let now = Date(timeIntervalSince1970: 1_800_000_000)

    @Test func theHeaderShowsStateTimePRsModelAndContext() throws {
        let since = ISO8601DateFormatter().string(from: now.addingTimeInterval(-252))
        let state = Seed.state(sessions: [SeedSession(
            id: "s1", name: "a", state: "waiting", model: "opus", contextLeft: 61, since: since,
            worktrees: [
                SeedWorktree(id: "w1", repo: "api", branch: "b", pr: 42, checks: "passing"),
                SeedWorktree(id: "w2", repo: "web", branch: "c", pr: 7, checks: "failing"),
            ]
        )])
        let header = Header(session: state.sessions[0], now: now)
        #expect(header.chip == "Waiting · 4m")
        #expect(header.style == StateStyle(state: "waiting"))
        #expect(header.prs == ["#42 ✓", "#7 ✗"])
        #expect(header.model == "opus")
        #expect(header.context == "61% context left")
    }

    @Test func aHeaderWithoutSinceOrContextLeavesThemOut() {
        let state = Seed.state(sessions: [SeedSession(id: "s1", name: "a", state: "idle")])
        let header = Header(session: state.sessions[0], now: now)
        #expect(header.chip == "Idle")
        #expect(header.context == nil)
        #expect(header.prs.isEmpty)
    }

    @Test func timeInStateReadsSecondsMinutesAndHours() {
        #expect(Header.elapsed(12) == "12s")
        #expect(Header.elapsed(60) == "1m")
        #expect(Header.elapsed(3_600 + 120) == "1h 2m")
        #expect(Header.elapsed(-5) == "0s")
    }

    @Test func quotaMetersShowUsedResetLowAndStale() throws {
        let state = Seed.state(sessions: [], limits: [
            SeedQuota(harness: "claude", label: "5h", left: 70, resetsAt: now.addingTimeInterval(3_600), staleAt: now.addingTimeInterval(60)),
            SeedQuota(harness: "codex", label: "7d", left: 12, resetsAt: now.addingTimeInterval(86_400), staleAt: now.addingTimeInterval(-60)),
        ])
        let utc = try #require(TimeZone(identifier: "UTC"))
        let meters = QuotaMeter.all(state.limits, now: now, timeZone: utc)
        #expect(meters.map(\.title) == ["Claude 5h", "Codex 7d"])
        #expect(meters.map(\.used) == [30, 88])
        #expect(meters.map(\.low) == [false, true])
        #expect(meters.map(\.stale) == [false, true])
        #expect(meters[0].caption == "30% · resets 09:00")
        #expect(meters[0].tone == .blue)
        #expect(meters[1].tone == .peach)
    }

    @Test func theViewSwitcherEnablesEveryViewForASessionAndNewIsOn() {
        let views = Toolbar.views(session: true)
        #expect(views.map(\.title) == ["Terminal", "Review", "Shell", "nvim"])
        #expect(views.map(\.enabled) == [true, true, true, true])
        #expect(views.allSatisfy { $0.help == nil })
        #expect(Toolbar.newEnabled)
    }

    @Test func withoutASessionTheSessionViewsAreDisabledAndSayWhy() {
        let views = Toolbar.views(session: false)
        #expect(views.map(\.enabled) == [true, false, false, false])
        #expect(views.map(\.help) == [
            nil,
            "Start or select a session to review its changes",
            "Start or select a session to open a shell in its worktree",
            "Start or select a session to open nvim in its worktree",
        ])
    }

    @Test(arguments: [MainView.review, .shell, .nvim])
    func aSessionShortcutWithoutASessionSaysWhy(view: MainView) {
        #expect(Toolbar.needsSession(view) == Toolbar.views(session: false).first { $0.title == view.rawValue }?.help)
    }

    @Test func theEmptyMainColumnTellsWhatToDo() {
        #expect(EmptyMain(connected: false, sessions: 0) == EmptyMain(title: "Waiting for the daemon", detail: nil, offersNew: false))
        #expect(EmptyMain(connected: true, sessions: 0) == EmptyMain(
            title: "No sessions yet", detail: "Start a session to see its terminal, review its changes, or open a shell or nvim.", offersNew: true))
        #expect(EmptyMain(connected: true, sessions: 3) == EmptyMain(
            title: "Select a session", detail: "Pick one in the sidebar (⌘1–9) to see its terminal, review, shell or nvim.", offersNew: true))
    }

    @Test func theConnectionBannerTellsRetryingFromStopped() {
        #expect(ConnectionBanner(.live) == nil)
        #expect(ConnectionBanner(.idle) == nil)
        let unavailable = ConnectionBanner(.unavailable("no agentws daemon is running"))
        #expect(unavailable?.title == "No agentws daemon · retrying")
        #expect(unavailable?.detail == "no agentws daemon is running")
        #expect(unavailable?.retrying == true)
        #expect(unavailable?.action == nil)
        #expect(unavailable?.tone == .peach)
        let mismatch = ConnectionBanner(.versionMismatch("daemon runs agentws v2 but this client is v1"))
        #expect(mismatch?.title == "Build mismatch · stopped")
        #expect(mismatch?.detail == "daemon runs agentws v2 but this client is v1")
        #expect(mismatch?.retrying == false)
        #expect(mismatch?.action == "Reconnect")
        #expect(mismatch?.tone == .red)
        #expect(ConnectionBanner(.reconnecting(in: .seconds(4)))?.title == "Reconnecting in 4 s")
        #expect(ConnectionBanner(.connecting)?.retrying == true)
    }
}
