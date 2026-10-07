import Foundation
import Testing
@testable import AgentwsKit

@MainActor
struct ShellNvimTests {
    let shellReply = #"{"pane":"%t1","dir":"/w/api","shown":true}"#
    let nvimReply = #"{"pane":"%n1","socket":"/tmp/s1.sock","shown":true}"#

    func make() -> (ShellNvim, FakeCaller) {
        let caller = FakeCaller()
        caller.replies["shell.focus"] = .success(shellReply)
        caller.replies["nvim.toggle"] = .success(nvimReply)
        return (ShellNvim(caller: caller), caller)
    }

    @Test func aShellOpensInTheSelectedWorktreeAndShowsItsPane() async throws {
        let (views, caller) = make()
        await views.toggleShell(session: "s1", worktree: "/w/api")
        let params = try #require(caller.calls.last?.params)
        #expect(caller.methods() == ["shell.focus"])
        #expect(params["session"] as? String == "s1")
        #expect(params["worktree"] as? String == "/w/api")
        #expect(views.view == .shell)
        #expect(views.pane(session: "s1", agent: "%a1") == "%t1")
    }

    @Test func aShellWithNoWorktreeLetsTheDaemonPickTheSessionRoot() async throws {
        let (views, caller) = make()
        await views.toggleShell(session: "s1")
        let params = try #require(caller.calls.last?.params)
        #expect(params["worktree"] as? String ?? "" == "")
        #expect(views.view == .shell)
    }

    @Test func closingTheShellViewLeavesTheShellRunning() async {
        let (views, caller) = make()
        await views.toggleShell(session: "s1")
        await views.toggleShell(session: "s1")
        #expect(views.view == .terminal)
        #expect(caller.methods() == ["shell.focus"])
        #expect(views.pane(session: "s1", agent: "%a1") == "%a1")
        await views.toggleShell(session: "s1")
        #expect(views.pane(session: "s1", agent: "%a1") == "%t1")
    }

    @Test func theShellPopupOverlaysTheCurrentView() async {
        let (views, _) = make()
        await views.toggleShell(session: "s1", popup: true)
        #expect(views.view == .terminal)
        #expect(views.popup == "%t1")
        await views.toggleShell(session: "s1", popup: true)
        #expect(views.popup == nil)
    }

    @Test func nvimShowsTheSessionsEditorAndClosingKeepsItRunning() async throws {
        let (views, caller) = make()
        await views.toggleNvim(session: "s1")
        #expect(caller.methods() == ["nvim.toggle"])
        #expect(try #require(caller.calls.last?.params)["session"] as? String == "s1")
        #expect(views.view == .nvim)
        #expect(views.pane(session: "s1", agent: "%a1") == "%n1")
        views.close()
        #expect(views.view == .terminal)
        #expect(caller.methods() == ["nvim.toggle"])
    }

    @Test func nvimOpenedFromReviewShowsTheNvimView() {
        let (views, caller) = make()
        views.showNvim(session: "s1", pane: "%n2")
        #expect(views.view == .nvim)
        #expect(views.pane(session: "s1", agent: "%a1") == "%n2")
        #expect(caller.calls.isEmpty)
    }

    @Test func aFailedNvimCallStaysOnTheTerminalWithTheDaemonsMessage() async {
        let (views, caller) = make()
        caller.replies["nvim.toggle"] = .failure(RPCError(code: "unavailable", message: "nvim is not on PATH"))
        await views.toggleNvim(session: "s1")
        #expect(views.view == .terminal)
        #expect(views.error == "nvim is not on PATH")
    }

    @Test func closingWhileAShellOrNvimCallIsPendingKeepsTheTerminal() async {
        let (views, caller) = make()
        caller.beforeReply = { views.close() }
        await views.toggleShell(session: "s1")
        #expect(views.view == .terminal)
        await views.toggleNvim(session: "s1")
        #expect(views.view == .terminal)
        caller.beforeReply = nil
        await views.toggleShell(session: "s1")
        #expect(views.view == .shell)
    }

    @Test func anotherSessionWithoutAShellFallsBackToItsAgentPane() async {
        let (views, _) = make()
        await views.toggleShell(session: "s1")
        #expect(views.pane(session: "s2", agent: "%a2") == "%a2")
    }
}
