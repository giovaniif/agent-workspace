import Foundation
import Testing
@testable import AgentwsKit

@MainActor
struct WorkspacePathTests {
    static let code = #"{"dirs":[{"Name":"api","Path":"/home/me/code/api","Git":1},{"Name":"Apps","Path":"/home/me/code/Apps","Git":0},{"Name":"api-eng-1","Path":"/home/me/code/api-eng-1","Git":2},{"Name":".cache","Path":"/home/me/code/.cache","Git":0},{"Name":"web","Path":"/home/me/code/web","Git":1}]}"#

    func input(_ text: String) -> (WorkspacePathInput, NewSessionCaller) {
        let caller = NewSessionCaller()
        caller.replies["workspace.dirs"] = .success(Self.code)
        let input = WorkspacePathInput(caller: caller)
        input.home = "/home/me"
        input.text = text
        return (input, caller)
    }

    @Test(arguments: [
        ("~", "/home/me", "", "/home/me"),
        ("~/code/a", "/home/me/code", "a", "/home/me/code/a"),
        ("code/", "/home/me/code", "", "/home/me/code"),
        ("./code/../src/x", "/home/me/src", "x", "/home/me/src/x"),
        ("/srv/repos/", "/srv/repos", "", "/srv/repos"),
    ])
    func typedPathsResolveAgainstTheServersHome(text: String, dir: String, prefix: String, path: String) {
        let parsed = ParsedPath.parse(text, home: "/home/me")
        #expect(parsed == ParsedPath(dir: dir, prefix: prefix, path: path))
    }

    @Test func typingListsTheMatchingFoldersMarkedRepoOrWorktree() async {
        let (input, caller) = input("~/code/a")
        await input.refresh()
        #expect(caller.params("workspace.dirs") == .object(["path": .string("/home/me/code")]))
        #expect(input.suggestions.map(\.name) == ["api", "api-eng-1", "Apps"])
        #expect(input.suggestions.map(\.mark) == ["repo", "worktree", ""])
    }

    @Test func hiddenFoldersShowOnlyForADotPrefix() async {
        let (input, _) = input("~/code/.")
        await input.refresh()
        #expect(input.suggestions.map(\.name) == [".cache"])
    }

    @Test func theFolderIsListedOnceWhileTheInputStaysInIt() async {
        let (input, caller) = input("~/code/a")
        await input.refresh()
        input.text = "~/code/ap"
        await input.refresh()
        #expect(caller.calls.filter { $0.method == "workspace.dirs" }.count == 1)
        #expect(input.suggestions.map(\.name) == ["api", "api-eng-1", "Apps"])
    }

    @Test func arrowsMoveTheHighlightAndAcceptOpensTheFolder() async {
        let (input, _) = input("~/code/a")
        await input.refresh()
        input.moveDown()
        input.moveDown()
        input.moveDown()
        #expect(input.highlight == 2)
        input.moveUp()
        #expect(input.accept())
        #expect(input.text == "~/code/api-eng-1/")
        #expect(input.highlight == 0)
    }

    @Test func acceptWithNoSuggestionDoesNothing() {
        let (input, _) = input("~/code/zz")
        #expect(!input.accept())
        #expect(input.text == "~/code/zz")
    }

    @Test func theLineUnderTheBoxNamesWhatThePathIs() async {
        let (input, caller) = input("~/code/web")
        await input.refresh()
        #expect(input.status == "/home/me/code/web · single repo")
        input.text = "~/code/Apps"
        #expect(input.status == "/home/me/code/Apps · folder")
        input.text = "~/code/"
        await input.refresh()
        #expect(input.status == "/home/me/code · orchestration root · 2 repos")
        caller.replies["workspace.dirs"] = .failure(RPCError(code: "not_found", message: "no such file"))
        input.text = "~/nope/x"
        await input.refresh()
        #expect(input.status == "/home/me/nope/x · no such folder")
        #expect(input.suggestions.isEmpty)
    }

    @Test func anEmptyInputShowsNothing() async {
        let (input, caller) = input("")
        await input.refresh()
        #expect(input.status == "")
        #expect(caller.calls.isEmpty)
    }
}
