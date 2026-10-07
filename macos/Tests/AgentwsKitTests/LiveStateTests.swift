import Foundation
import Testing
@testable import AgentwsKit

struct LiveStateTests {
    @Test func applyingTheRecordedStreamYieldsTheExpectedState() throws {
        var state = try Goldens.state()
        for diff in try Goldens.diffs() {
            state.apply(diff)
        }
        #expect(state.seq == 15)
        #expect(state.sessions.map(\.id) == ["s1", "s2"])
        #expect(state.sessions.first { $0.id == "s1" }?.name == "Add login")
        #expect(state.sessions.first { $0.id == "s1" }?.order == 1)
        #expect(state.sessions.first { $0.id == "s2" }?.state == "waiting")
        #expect(state.sessions.first { $0.id == "s2" }?.banner == "waiting")
        #expect(state.worktrees.first { $0.id == "w1" }?.pr?.number == 12)
        #expect(state.events.map(\.sessionID) == ["s2"])
        #expect(state.subagents.map(\.state) == ["stopped"])
    }

    @Test func sortedSessionsFollowTheirOrder() throws {
        let state = try Goldens.state()
        #expect(state.sortedSessions.map(\.id) == ["s3", "s1", "s2"])
    }

    @Test func keepsTheLastTwentyEventsOfASession() throws {
        var state = try Goldens.state()
        for i in 0..<25 {
            var diff = ViewDiff(seq: UInt64(20 + i))
            diff.event = SessionEvent(sessionID: "s1", at: "2026-10-01T12:00:00Z", kind: "stop", tool: "", detail: "", text: "\(i)")
            state.apply(diff)
        }
        let s1 = state.events.filter { $0.sessionID == "s1" }
        #expect(s1.count == 20)
        #expect(s1.first?.text == "5")
        #expect(state.events.contains { $0.sessionID == "s3" })
    }

    @Test func listDiffsReplaceTheWholeList() throws {
        var state = try Goldens.state()
        var diff = ViewDiff(seq: 30)
        diff.sends = [QueuedSend(id: "q1", session: "s1", text: "go", queuedAt: "2026-10-01T12:00:00Z")]
        state.apply(diff)
        #expect(state.sends.map(\.id) == ["q1"])
        diff.sends = []
        state.apply(diff)
        #expect(state.sends.isEmpty)
    }

    @Test func removingAWorktreeAndAWorkspaceDropsThem() throws {
        var state = try Goldens.state()
        var diff = ViewDiff(seq: 31)
        diff.removedWorktree = "w2"
        state.apply(diff)
        diff = ViewDiff(seq: 32)
        diff.removedWorkspace = "/w/api"
        state.apply(diff)
        #expect(state.worktrees.map(\.id) == ["w1"])
        #expect(state.workspaces.isEmpty)
    }
}
