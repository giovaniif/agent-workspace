import Foundation
import Testing
@testable import AgentwsKit

@MainActor
final class FakeCaller: Caller {
    var replies: [String: Result<String, RPCError>] = [:]
    var calls: [(method: String, params: [String: Any])] = []
    var beforeReply: (() -> Void)?

    func call<Params: Encodable & Sendable, Reply: Decodable & Sendable>(_ method: String, params: Params) async throws -> Reply {
        let data = try JSONEncoder().encode(params)
        calls.append((method, (try JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]))
        beforeReply?()
        switch replies[method] {
        case let .success(json)?: return try JSONDecoder().decode(Reply.self, from: Data(json.utf8))
        case let .failure(error)?: throw AgentwsError.rpc(error)
        case nil: throw AgentwsError.disconnected
        }
    }

    func methods() -> [String] { calls.map(\.method) }
}

let openReply = #"""
{"scope":"uncommitted","worktrees":[{"Worktree":{"ID":"/w/api-feat","Repo":"api","Path":"/w/api-feat","Branch":"feat","PR":{"Number":42}},"From":"HEAD","Files":[
 {"Path":"main.go","OldPath":"","Status":"M","Added":1,"Deleted":1,"Binary":false,"Blob":"blob1","Hunks":[
  {"Header":"@@ -1,2 +1,2 @@","Lines":[{"Kind":32,"Old":1,"New":1,"Text":"package main"},{"Kind":45,"Old":2,"New":0,"Text":"var x = 1"},{"Kind":43,"Old":0,"New":2,"Text":"var y = 1"}]}]}
],"Err":""}],"viewed":[],"draft":{"id":"","session":"","status":"","comments":null,"turns":null}}
"""#

let draftReply = #"""
{"id":"d1","session":"s1","status":"open","comments":[{"id":"c1","worktree":"/w/api-feat","path":"main.go","start":2,"end":2,"code":["var y = 1"],"body":"call it y?"}],"turns":null}
"""#

@MainActor
struct ReviewControllerTests {
    func opened() async -> (ReviewController, FakeCaller) {
        let caller = FakeCaller()
        caller.replies["review.open"] = .success(openReply)
        let controller = ReviewController(session: "s1", caller: caller)
        await controller.open()
        return (controller, caller)
    }

    var key: FileKey { FileKey(worktree: "/w/api-feat", path: "main.go") }

    @Test func openAsksForTokensInTheChosenScope() async throws {
        let caller = FakeCaller()
        caller.replies["review.open"] = .success(openReply)
        let controller = ReviewController(session: "s1", caller: caller)
        controller.screen.scope = .uncommitted
        await controller.open()
        let params = try #require(caller.calls.first?.params)
        #expect(caller.methods() == ["review.open"])
        #expect(params["session"] as? String == "s1")
        #expect(params["scope"] as? String == "uncommitted")
        #expect(params["tokens"] as? Bool == true)
        #expect(params["worktree"] as? String == "")
        #expect(controller.screen.tree.map(\.title) == ["api #42"])
        #expect(controller.screen.selected == key)
        #expect(controller.screen.error == nil)
    }

    @Test func steppingTheScopeReopens() async throws {
        let (controller, caller) = await opened()
        #expect(controller.screen.scope == .lastTurn)
        await controller.stepScope(1)
        #expect(controller.screen.scope == .uncommitted)
        #expect(caller.calls.last?.params["scope"] as? String == "uncommitted")
        await controller.stepScope(-2)
        #expect(caller.calls.last?.params["scope"] as? String == "branch")
    }

    @Test func aCommentCarriesTheWorktreeFileAndLines() async throws {
        let (controller, caller) = await opened()
        caller.replies["review.comment"] = .success(draftReply)
        let lines = try #require(controller.screen.selectedFile?.file.hunks.first?.lines)
        await controller.comment(on: key, lines: [lines[2]], body: " call it y? ")
        let params = try #require(caller.calls.last?.params)
        #expect(caller.calls.last?.method == "review.comment")
        #expect(params["session"] as? String == "s1")
        #expect(params["worktree"] as? String == "/w/api-feat")
        #expect(params["path"] as? String == "main.go")
        #expect(params["start_line"] as? Int == 2)
        #expect(params["end_line"] as? Int == 2)
        #expect(params["code"] as? String == "var y = 1")
        #expect(params["body"] as? String == "call it y?")
        #expect(params["removed"] as? Bool ?? false == false)
        #expect(controller.screen.draftRows.map(\.label) == ["api · main.go:2"])
    }

    @Test func aRangeOfRemovedLinesIsMarkedRemoved() async throws {
        let (controller, caller) = await opened()
        caller.replies["review.comment"] = .success(draftReply)
        let lines = try #require(controller.screen.selectedFile?.file.hunks.first?.lines)
        await controller.comment(on: key, lines: [lines[1]], body: "keep")
        let params = try #require(caller.calls.last?.params)
        #expect(params["start_line"] as? Int == 2)
        #expect(params["removed"] as? Bool == true)
    }

    @Test func sendPostsTheDraftWithTheNoteAsOneCall() async throws {
        let (controller, caller) = await opened()
        caller.replies["review.comment"] = .success(draftReply)
        caller.replies["review.send"] = .success(draftReply.replacingOccurrences(of: #""status":"open""#, with: #""status":"sent""#))
        let lines = try #require(controller.screen.selectedFile?.file.hunks.first?.lines)
        await controller.comment(on: key, lines: [lines[2]], body: "call it y?")
        controller.screen.note = "Also add a test."
        await controller.send()
        #expect(caller.methods() == ["review.open", "review.comment", "review.send"])
        let params = try #require(caller.calls.last?.params)
        #expect(params["session"] as? String == "s1")
        #expect(params["note"] as? String == "Also add a test.")
        #expect(controller.screen.note == "")
        #expect(controller.screen.draft?.status == "sent")
        #expect(!controller.screen.canSend)
    }

    @Test func aNoteEditedWhileSendingIsKept() async throws {
        let (controller, caller) = await opened()
        caller.replies["review.comment"] = .success(draftReply)
        caller.replies["review.send"] = .success(draftReply)
        let lines = try #require(controller.screen.selectedFile?.file.hunks.first?.lines)
        await controller.comment(on: key, lines: [lines[2]], body: "call it y?")
        controller.screen.note = "first"
        caller.beforeReply = { controller.screen.note = "second" }
        await controller.send()
        #expect(caller.calls.last?.params["note"] as? String == "first")
        #expect(controller.screen.note == "second")
    }

    @Test func aRefusedHunkShowsGitsMessageAndChangesNothing() async throws {
        let (controller, caller) = await opened()
        let before = controller.screen.result
        caller.replies["review.hunk"] = .failure(RPCError(code: "failed", message: "error: patch failed: main.go:1\nerror: main.go: patch does not apply"))
        await controller.hunk(key, index: 0, action: .revert)
        #expect(caller.methods() == ["review.open", "review.hunk"])
        #expect(controller.screen.error == "error: patch failed: main.go:1\nerror: main.go: patch does not apply")
        #expect(controller.screen.result == before)
    }

    @Test func aStagedHunkSendsTheFileAndReloads() async throws {
        let (controller, caller) = await opened()
        caller.replies["review.hunk"] = .success("{}")
        await controller.hunk(key, index: 0, action: .stage)
        #expect(caller.methods() == ["review.open", "review.hunk", "review.open"])
        let params = caller.calls[1].params
        #expect(params["session"] as? String == "s1")
        #expect(params["worktree"] as? String == "/w/api-feat")
        #expect(params["hunk"] as? Int == 0)
        #expect(params["action"] as? String == "stage")
        let file = try #require(params["file"] as? [String: Any])
        #expect(file["Path"] as? String == "main.go")
        #expect(file["Status"] as? String == "M")
        #expect(file["Blob"] as? String == "blob1")
        let hunks = try #require(file["Hunks"] as? [[String: Any]])
        #expect(hunks.first?["Header"] as? String == "@@ -1,2 +1,2 @@")
        let lines = try #require(hunks.first?["Lines"] as? [[String: Any]])
        #expect(lines.map { $0["Kind"] as? Int } == [32, 45, 43])
        #expect(controller.screen.error == nil)
    }

    @Test func viewedMarksTheFileWithItsBlob() async throws {
        let (controller, caller) = await opened()
        caller.replies["review.viewed"] = .success("{}")
        await controller.toggleViewed(key)
        let params = try #require(caller.calls.last?.params)
        #expect(caller.calls.last?.method == "review.viewed")
        #expect(params["viewed"] as? Bool == true)
        let mark = try #require(params["mark"] as? [String: Any])
        #expect(mark["Worktree"] as? String == "/w/api-feat")
        #expect(mark["Path"] as? String == "main.go")
        #expect(mark["Blob"] as? String == "blob1")
        #expect(controller.screen.tree.first?.files.first?.viewed == true)
        await controller.toggleViewed(key)
        #expect(caller.calls.last?.params["viewed"] as? Bool == false)
        #expect(controller.screen.tree.first?.files.first?.viewed == false)
    }

    @Test func openInNvimNamesTheWorktreeFileAndLine() async throws {
        let (controller, caller) = await opened()
        caller.replies["nvim.open"] = .success(#"{"pane":"%9","socket":"/tmp/s.sock","shown":true}"#)
        let opened = await controller.openInNvim(key, line: 2)
        #expect(opened)
        let params = try #require(caller.calls.last?.params)
        #expect(caller.calls.last?.method == "nvim.open")
        #expect(params["session"] as? String == "s1")
        #expect(params["worktree"] as? String == "/w/api-feat")
        #expect(params["path"] as? String == "main.go")
        #expect(params["line"] as? Int == 2)
    }

    @Test func aReplyForAScopeNoLongerChosenIsDropped() async {
        let caller = FakeCaller()
        caller.replies["review.open"] = .success(openReply)
        let controller = ReviewController(session: "s1", caller: caller)
        caller.beforeReply = { controller.screen.scope = .branch }
        await controller.open()
        #expect(controller.screen.result == nil)
        caller.replies["review.open"] = .failure(RPCError(code: "failed", message: "stale"))
        caller.beforeReply = { controller.screen.scope = .uncommitted }
        await controller.open()
        #expect(controller.screen.error == nil)
    }

    @Test func reopeningAfterASendClearsTheSentDraft() async throws {
        let (controller, caller) = await opened()
        caller.replies["review.comment"] = .success(draftReply)
        caller.replies["review.send"] = .success(draftReply.replacingOccurrences(of: #""status":"open""#, with: #""status":"sent""#))
        let lines = try #require(controller.screen.selectedFile?.file.hunks.first?.lines)
        await controller.comment(on: key, lines: [lines[2]], body: "call it y?")
        await controller.send()
        #expect(controller.screen.draft?.status == "sent")
        await controller.open()
        #expect(controller.screen.draft == nil)
        #expect(controller.screen.draftRows.isEmpty)
    }

    @Test func aFailedOpenKeepsTheMessage() async {
        let caller = FakeCaller()
        caller.replies["review.open"] = .failure(RPCError(code: "unavailable", message: "review is not enabled"))
        let controller = ReviewController(session: "s1", caller: caller)
        await controller.open()
        #expect(controller.screen.error == "review is not enabled")
        #expect(controller.screen.result == nil)
    }
}
