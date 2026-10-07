import Foundation
import Testing
import AgentwsKit

struct DiskTests {
    let now = Date(timeIntervalSince1970: 1_800_000_000)

    func view(_ rows: [[String: Any]], reclaimable: Int64 = 0, reclaimablePending: Int = 0, total: Int64 = 0, totalPending: Int = 0, deps: Any = NSNull(), recent: [[String: Any]] = [], nextCleanup: Any = NSNull()) throws -> DiskView {
        let object: [String: Any] = [
            "next_cleanup": nextCleanup,
            "free": 42_000_000_000, "total": 500_000_000_000, "auto_clean_every": 600_000_000_000,
            "deps_store": deps, "rows": rows, "recent": recent,
            "reclaimable": reclaimable, "reclaimable_pending": reclaimablePending,
            "worktrees_size": total, "worktrees_pending": totalPending,
        ]
        return try JSONDecoder().decode(DiskView.self, from: JSONSerialization.data(withJSONObject: object))
    }

    func row(_ id: String, size: Int64, action: String, reason: String) -> [String: Any] {
        ["WorktreeID": id, "Size": size, "Action": action, "Reason": reason]
    }

    @Test func theDiskViewDecodesTheDaemonsFieldNames() throws {
        let v = try view([row("w1", size: 1_500_000, action: "keep", reason: "not merged")], deps: ["path": "/deps", "size": 900])
        #expect(v.free == 42_000_000_000)
        #expect(v.autoCleanEvery == .seconds(600))
        #expect(v.depsStore?.path == "/deps")
        #expect(v.rows[0].worktreeID == "w1")
        #expect(v.rows[0].action == .keep)
        #expect(v.rows[0].reason == "not merged")
    }

    @Test func tilesMarkWhatIsStillMeasuring() throws {
        let measuring = DiskTiles(try view([], reclaimable: 340_000_000, reclaimablePending: 1, total: 0, totalPending: 2, deps: ["path": "/deps", "size": -1]))
        #expect(measuring.free == "42 GB free")
        #expect(measuring.worktrees == "…")
        #expect(measuring.reclaimable == "340 MB+")
        #expect(measuring.depsStore == "…")
        #expect(measuring.schedule == "every 10m")
        let done = DiskTiles(try view([], reclaimable: 340_000_000, total: 1_200_000_000))
        #expect(done.worktrees == "1.2 GB")
        #expect(done.reclaimable == "340 MB")
        #expect(done.depsStore == nil)
    }

    @Test func sizesReadInDecimalUnits() {
        #expect(DiskTiles.size(-1) == "…")
        #expect(DiskTiles.size(512) == "512 B")
        #expect(DiskTiles.size(2_500) == "2.5 KB")
        #expect(DiskTiles.size(40_000_000) == "40 MB")
    }

    @Test func tableRowsSayStateSizePortsAndWhatCleanupWillDo() throws {
        let state = Seed.window
        let v = try view([
            row("w1", size: -1, action: "keep", reason: "its session is live"),
            row("w4", size: 40_000_000, action: "remove", reason: "merged into the default branch"),
            row("w5", size: 7_000, action: "backup_then_ask", reason: "2 uncommitted changes"),
            row("gone", size: 1, action: "keep", reason: "x"),
        ])
        let rows = DiskTable.rows(v, state: state)
        #expect(rows.map(\.id) == ["w1", "w4", "w5", "gone"])
        #expect(rows[0].worktree == "api-42-login-redirect")
        #expect(rows[0].session == "fix login redirect")
        #expect(rows[0].pr == "#42")
        #expect(rows[0].status == "Open")
        #expect(rows[0].size == "…")
        #expect(rows[0].ports == ":3000")
        #expect(rows[0].plan == "kept: its session is live")
        #expect(rows[1].status == "Merged · clean")
        #expect(rows[1].plan == "removes in the next cleanup")
        #expect(rows[2].status == "Merged · dirty")
        #expect(rows[2].plan == "back up then ask")
        #expect(rows[3].worktree == "gone")
        #expect(rows[3].status == "No PR")
    }

    @Test func aMergedCleanRowSaysWhenTheNextCleanupRemovesIt() throws {
        let v = try view([row("w4", size: 1, action: "remove", reason: "merged")], nextCleanup: "2026-10-01T14:30:05.123456789Z")
        let rows = DiskTable.rows(v, state: Seed.window, timeZone: TimeZone(identifier: "UTC")!)
        #expect(rows[0].plan == "removes at 14:30")
    }

    @Test func aDetachedWorktreeIsBackedUpToABranchFirst() throws {
        var state = Seed.window
        state.worktrees[0].branch = ""
        state.worktrees[0].pr = nil
        let rows = DiskTable.rows(try view([row("w1", size: 5, action: "backup_then_ask", reason: "detached")]), state: state)
        #expect(rows[0].status == "Detached")
        #expect(rows[0].plan == "backup branch first")
    }

    @Test func aDirtyMergedWorktreeCanOnlyBeRemovedThroughBackUpAndRemove() throws {
        let rows = DiskTable.rows(try view([row("w5", size: 7, action: "backup_then_ask", reason: "dirty")]), state: Seed.window)
        let kinds = DiskActions.for(rows[0]).map(\.kind)
        #expect(kinds.contains(.backUpAndRemove))
        #expect(!kinds.contains(.remove))
    }

    @Test func aCleanMergedWorktreeIsRemovedWithoutBackup() throws {
        let rows = DiskTable.rows(try view([row("w4", size: 7, action: "remove", reason: "merged")]), state: Seed.window)
        let actions = DiskActions.for(rows[0])
        #expect(actions.map(\.kind) == [.goToSession, .openShell, .remove])
        #expect(actions[2].params == CleanupWorktreeParams(path: "/w/docs-quickstart", backup: false))
    }

    @Test func aKeptWorktreeOffersNoRemoval() throws {
        let rows = DiskTable.rows(try view([row("w1", size: 7, action: "keep", reason: "live")]), state: Seed.window)
        #expect(DiskActions.for(rows[0]).map(\.kind) == [.goToSession, .openShell, .killDevServers])
    }

    @Test func everyDestructiveActionAsksFirstAndSaysWhatWillHappen() throws {
        let rows = DiskTable.rows(try view([
            row("w1", size: 7, action: "keep", reason: "live"),
            row("w4", size: 7, action: "remove", reason: "merged"),
            row("w5", size: 7, action: "backup_then_ask", reason: "dirty"),
        ]), state: Seed.window)
        let all = rows.flatMap { DiskActions.for($0) }
        for action in all where action.kind.destructive {
            #expect(action.confirm != nil, "\(action.kind) must confirm")
        }
        for action in all {
            #expect(!action.outcome.isEmpty)
        }
        let kill = try #require(all.first { $0.kind == .killDevServers })
        #expect(kill.pgids == [1])
        #expect(kill.outcome == "Stops the dev server on :3000.")
        let backup = try #require(all.first { $0.kind == .backUpAndRemove })
        #expect(backup.params == CleanupWorktreeParams(path: "/w/api-cache-layer", backup: true))
        #expect(backup.title == "Back up and remove…")
    }

    @Test func cleanupParamsEncodeAsTheDaemonExpects() throws {
        let data = try JSONEncoder().encode(CleanupWorktreeParams(path: "/w/a", backup: true))
        let object = try JSONSerialization.jsonObject(with: data) as? [String: Any]
        #expect(object?["path"] as? String == "/w/a")
        #expect(object?["backup"] as? Bool == true)
        let kill = try JSONSerialization.jsonObject(with: JSONEncoder().encode(PortsKillParams(pgids: [4, 5]))) as? [String: Any]
        #expect(kill?["pgids"] as? [Int] == [4, 5])
    }

    @Test func thePortsTabListsEveryDevServerWithItsWorktree() {
        let ports = DiskPorts.rows(Seed.window)
        #expect(ports.map(\.port) == [3000, 5173])
        #expect(ports[0].worktree == "api-42-login-redirect")
        #expect(ports[0].session == "fix login redirect")
        #expect(ports[0].command == "node")
    }

    @Test func recentlyCleanedReadsTimeBranchAndOutcome() throws {
        let at = ISO8601DateFormatter().string(from: now.addingTimeInterval(-300))
        let v = try view([], recent: [["at": at, "path": "/w/old", "branch": "old", "action": "remove", "outcome": "removed"]])
        let rows = DiskRecent.rows(v, now: now)
        #expect(rows.map(\.worktree) == ["old"])
        #expect(rows[0].branch == "old")
        #expect(rows[0].when == "5m ago")
        #expect(rows[0].outcome == "removed")
    }
}
