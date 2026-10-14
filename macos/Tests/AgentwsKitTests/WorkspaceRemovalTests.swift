import Foundation
import Testing
@testable import AgentwsKit

struct WorkspaceRemovalTests {
    static func sessions(_ dirs: [(String, Bool)]) -> [Session] {
        var state = Seed.state(sessions: dirs.indices.map { SeedSession(id: "s\($0)", name: "s\($0)", state: "running", where: "api@x") })
        for (index, dir) in dirs.enumerated() {
            state.sessions[index].dir = dir.0
            state.sessions[index].ended = dir.1
        }
        return state.sessions
    }

    @Test func liveSessionsInTheWorkspaceOrBelowItCount() {
        let sessions = Self.sessions([
            ("/home/me", false),
            ("/home/me/api", false),
            ("/home/me/api", true),
            ("/home/meow", false),
            ("/srv", false),
        ])
        #expect(WorkspaceRemoval(root: "/home/me", sessions: sessions).live == 2)
    }

    @Test func theConfirmationSaysThatNoFilesAreDeleted() {
        let removal = WorkspaceRemoval(root: "/home/me", sessions: [])
        #expect(removal.title == "Remove /home/me from agentws?")
        #expect(removal.message == "agentws stops using this workspace. No files, worktrees or branches on the server are deleted.")
        #expect(removal.button == "Remove")
    }

    @Test func theConfirmationNamesHowManyLiveSessionsUseTheWorkspace() {
        let one = WorkspaceRemoval(root: "/home/me", sessions: Self.sessions([("/home/me", false)]))
        #expect(one.message.hasPrefix("1 live session uses this workspace. "))
        let two = WorkspaceRemoval(root: "/home/me", sessions: Self.sessions([("/home/me", false), ("/home/me/a", false)]))
        #expect(two.message.hasPrefix("2 live sessions use this workspace. "))
    }
}
