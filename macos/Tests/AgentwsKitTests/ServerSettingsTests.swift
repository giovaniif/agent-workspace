import Foundation
import Testing
@testable import AgentwsKit

@MainActor
final class FakeCaller: RPCCalling {
    var replies: [String: String] = [:]
    var failures: [String: AgentwsError] = [:]
    private(set) var calls: [(method: String, params: String)] = []

    func call<Params: Encodable & Sendable, Result: Decodable & Sendable>(_ method: String, params: Params) async throws -> Result {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        calls.append((method, String(decoding: try encoder.encode(params), as: UTF8.self)))
        if let failure = failures[method] { throw failure }
        let reply = try #require(replies[method], "no reply for \(method)")
        return try JSONDecoder().decode(Result.self, from: Data(reply.utf8))
    }
}

@MainActor
struct ServerSettingsTests {
    static let status = #"""
    {"done":true,"harnesses":{
      "claude":{"installed":true,"file":"/u/.claude/settings.json","backup":"/u/.claude/settings.json.agentws.bak"},
      "codex":{"installed":false,"file":"/u/.codex/hooks.json","err":"hooks.json is not valid JSON"},
      "omp":{"installed":false,"file":""}},
     "nvim":{"on_path":true,"configured":false,"config_file":"","plugin_dir":"/u/.local/share/agentws/nvim","plugin_found":true}}
    """#

    static let workspaces = #"""
    {"workspaces":[
      {"Root":"/u/code/platform","Kind":"orchestration","LastUsed":"2026-10-01T10:00:00Z","Repos":[
        {"Name":"api","Path":"/u/code/platform/api","DefaultBranch":"main","Branch":"main","ChangedFiles":0},
        {"Name":"web","Path":"/u/code/platform/web","DefaultBranch":"main","Branch":"42-retry","ChangedFiles":3}]},
      {"Root":"/u/code/notes","Kind":"single","LastUsed":"0001-01-01T00:00:00Z","Repos":null}],
     "last_used":"/u/code/platform"}
    """#

    func caller() -> FakeCaller {
        let fake = FakeCaller()
        fake.replies["onboarding.status"] = Self.status
        fake.replies["workspace.list"] = Self.workspaces
        return fake
    }

    @Test func refreshReadsTheAgentsAndWorkspacesOfTheServer() async throws {
        let fake = caller()
        let server = ServerSettings(caller: fake)
        await server.refresh()

        let agents = try #require(server.agents)
        #expect(agents.harnesses["claude"]?.installed == true)
        #expect(agents.harnesses["codex"]?.err == "hooks.json is not valid JSON")
        #expect(agents.nvim.pluginFound && !agents.nvim.configured)
        #expect(server.workspaces.map(\.root) == ["/u/code/platform", "/u/code/notes"])
        #expect(server.workspaces[0].summary == "orchestration root · 2 repos")
        #expect(server.workspaces[1].summary == "single repo")
        #expect(server.workspaces[0].repos.map(\.name) == ["api", "web"])
        #expect(server.lastUsed == "/u/code/platform")
        #expect(server.error == nil)
    }

    @Test func removingHooksReportsTheBackupAndReinstallPutsThemBack() async throws {
        let fake = caller()
        fake.replies["onboarding.remove"] = #"{"installed":false,"file":"/u/.claude/settings.json","backup":"/u/.claude/settings.json.agentws-removed-1.bak"}"#
        fake.replies["onboarding.install"] = #"{"installed":true,"file":"/u/.claude/settings.json"}"#
        let server = ServerSettings(caller: fake)

        await server.removeHooks("claude")
        #expect(server.notice == "Hooks removed. Backup: /u/.claude/settings.json.agentws-removed-1.bak")
        #expect(fake.calls.first?.method == "onboarding.remove")
        #expect(fake.calls.first?.params == #"{"harness":"claude"}"#)
        #expect(fake.calls.last?.method == "workspace.list")

        await server.installHooks("claude")
        #expect(fake.calls.map(\.method).contains("onboarding.install"))
        #expect(server.notice == "Hooks installed in /u/.claude/settings.json")
    }

    @Test func workspacesAreAddedAndRemovedOnTheServer() async {
        let fake = caller()
        fake.replies["workspace.add"] = #"{"Root":"/u/code/new","Kind":"single","LastUsed":"0001-01-01T00:00:00Z","Repos":[]}"#
        fake.replies["workspace.remove"] = "{}"
        let server = ServerSettings(caller: fake)

        await server.addWorkspace("/u/code/new")
        await server.removeWorkspace("/u/code/notes")
        let sent = fake.calls.filter { $0.method.hasPrefix("workspace.") && $0.method != "workspace.list" }
        #expect(sent.map(\.method) == ["workspace.add", "workspace.remove"])
        #expect(sent.map(\.params) == [#"{"path":"\/u\/code\/new"}"#, #"{"root":"\/u\/code\/notes"}"#])
    }

    @Test func aFailedCallIsShownAndTheLastKnownValuesStay() async {
        let fake = caller()
        let server = ServerSettings(caller: fake)
        await server.refresh()
        fake.failures["onboarding.remove"] = .rpc(RPCError(code: "failed", message: "settings.json is not valid JSON"))
        await server.removeHooks("claude")
        #expect(server.error == "settings.json is not valid JSON")
        #expect(server.agents?.harnesses["claude"]?.installed == true)
    }
}
