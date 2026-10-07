import Foundation
import Testing
@testable import AgentwsKit

@MainActor
final class FakeCaller: Calling {
    var replies: [String: Result<String, RPCError>] = [:]
    private(set) var calls: [(method: String, params: JSONValue)] = []

    func call<Params: Encodable & Sendable, Answer: Decodable & Sendable>(_ method: String, params: Params) async throws -> Answer {
        let sent = try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(params))
        calls.append((method, sent))
        switch replies[method] {
        case let .success(json):
            return try JSONDecoder().decode(Answer.self, from: Data(json.utf8))
        case let .failure(error):
            throw AgentwsError.rpc(error)
        case nil:
            throw AgentwsError.rpc(RPCError(code: "unknown_method", message: method))
        }
    }

    func params(_ method: String) -> JSONValue? {
        calls.last { $0.method == method }?.params
    }
}

enum NewSessionFixtures {
    static let options = """
    {"harnesses":[
      {"harness":"claude","name":"Claude Code","tag":"CC","models":["opus","sonnet","haiku"],"efforts":["low","medium","high","xhigh","max"],"model":"opus","effort":"xhigh"},
      {"harness":"codex","name":"Codex","tag":"CX","models":["gpt-6-sol","gpt-5.5"],"efforts":["low","medium","high"],"model":"gpt-6-sol","effort":"high"}
    ],"max_parallel":4}
    """

    static func workspaces() throws -> [Workspace] {
        let json = """
        [
          {"Root":"/src/api","Kind":"single","Repos":null,"LastUsed":"2026-10-01T09:00:00Z"},
          {"Root":"/src/acme","Kind":"orchestration","Repos":[{},{},{}],"LastUsed":"2026-10-05T09:00:00Z"},
          {"Root":"/src/web","Kind":"single","Repos":null,"LastUsed":"0001-01-01T00:00:00Z"}
        ]
        """
        return try JSONDecoder().decode([Workspace].self, from: Data(json.utf8))
    }

    static func quota(_ harness: String, _ label: String, left: Int) -> JSONValue {
        .object([
            "Harness": .string(harness), "label": .string(label), "LeftPercent": .number(Double(left)), "low": .bool(left < 20),
        ])
    }

    @MainActor
    static func loaded(_ caller: FakeCaller, limits: [JSONValue] = []) async throws -> NewSession {
        caller.replies["session.options"] = .success(options)
        let model = NewSession(caller: caller)
        var state = Seed.state(sessions: [])
        state.workspaces = try workspaces()
        state.limits = limits
        await model.load(state: state)
        return model
    }
}

@MainActor
struct NewSessionTests {
    @Test func theLastUsedWorkspaceIsPreselectedAndEachShowsItsKind() async throws {
        let model = try await NewSessionFixtures.loaded(FakeCaller())
        #expect(model.form.workspace == "/src/acme")
        #expect(model.form.workspaces.map(\.root) == ["/src/acme", "/src/api", "/src/web"])
        #expect(model.form.workspaces.map(\.detail) == ["orchestration root · 3 repos", "single repo", "single repo"])
        #expect(model.form.workspaces.first?.name == "acme")
    }

    @Test func defaultsComeFromTheServersDefaultsTables() async throws {
        let model = try await NewSessionFixtures.loaded(FakeCaller())
        #expect(model.form.harness == "claude")
        #expect(model.form.model == "opus")
        #expect(model.form.effort == "xhigh")
        model.form.choose(harness: "codex")
        #expect(model.form.model == "gpt-6-sol")
        #expect(model.form.effort == "high")
        #expect(model.form.chosen?.models == ["gpt-6-sol", "gpt-5.5"])
        #expect(model.maxParallel == 4)
    }

    @Test func aLowHarnessOffersOneSwitchToTheOther() async throws {
        let limits = [
            NewSessionFixtures.quota("claude", "5h", left: 12),
            NewSessionFixtures.quota("claude", "7d", left: 60),
            NewSessionFixtures.quota("codex", "7d", left: 70),
        ]
        let model = try await NewSessionFixtures.loaded(FakeCaller(), limits: limits)
        let warning = try #require(model.warning)
        #expect(warning.message == "Claude Code 5h quota is low: 12% left")
        #expect(warning.button == "Switch to Codex")
        model.takeSwitch()
        #expect(model.form.harness == "codex")
        #expect(model.form.model == "gpt-6-sol")
        #expect(model.warning == nil)
    }

    @Test func noSwitchIsOfferedWhenTheOtherHarnessIsLowOrUnreported() async throws {
        let bothLow = [NewSessionFixtures.quota("claude", "5h", left: 5), NewSessionFixtures.quota("codex", "7d", left: 9)]
        let low = try await NewSessionFixtures.loaded(FakeCaller(), limits: bothLow)
        #expect(low.warning?.message == "Claude Code 5h quota is low: 5% left")
        #expect(low.warning?.button == nil)
        let fine = try await NewSessionFixtures.loaded(FakeCaller(), limits: [NewSessionFixtures.quota("claude", "5h", left: 50)])
        #expect(fine.warning == nil)
    }

    @Test(arguments: [
        ("/src/api", "fix the login redirect"),
        ("/src/acme", "rename the billing tables"),
        ("/src/api", "https://github.com/acme/api/pull/42"),
        ("/src/acme", "https://linear.app/acme/issue/ENG-7/billing"),
    ])
    func startSendsTheFormAndFocusesTheNewSession(workspace: String, item: String) async throws {
        let caller = FakeCaller()
        let model = try await NewSessionFixtures.loaded(caller)
        caller.replies["session.new"] = .success(#"{"ID":"s9","State":"idle"}"#)
        caller.replies["session.focus"] = .success("{}")
        model.form.workspace = workspace
        model.form.workItem = "  " + item + "\n"
        model.form.effort = "medium"
        let started = await model.start()
        #expect(started == "s9")
        #expect(model.failure == nil)
        #expect(caller.params("session.new") == .object([
            "workspace": .string(workspace), "work_item": .string(item), "harness": .string("claude"),
            "model": .string("opus"), "effort": .string("medium"),
        ]))
        #expect(caller.params("session.focus") == .object(["id": .string("s9")]))
    }

    @Test func anEmptyWorkItemCannotStart() async throws {
        let caller = FakeCaller()
        let model = try await NewSessionFixtures.loaded(caller)
        model.form.workItem = "   "
        #expect(!model.form.canStart)
        #expect(await model.start() == nil)
        #expect(caller.params("session.new") == nil)
    }

    @Test func aSetupFailureShowsItsOutputInTheSheet() async throws {
        let caller = FakeCaller()
        let model = try await NewSessionFixtures.loaded(caller)
        let message = "setup /h/api/eng-7: exit status 1: bun install\nerror: lockfile had changes"
        caller.replies["session.new"] = .failure(RPCError(code: "failed", message: message))
        model.form.workItem = "eng-7"
        #expect(await model.start() == nil)
        #expect(model.failure == message)
        #expect(!model.starting)
    }

    @Test func theWorkItemResolvesIntoACardNamingTheBranchAndAnExistingWorktree() async throws {
        let caller = FakeCaller()
        let model = try await NewSessionFixtures.loaded(caller)
        caller.replies["session.resolve"] = .success(
            #"{"source":"linear","ref":"ENG-7","title":"Billing export","worktree":"42-login-redirect","workspace":"/src/acme"}"#
        )
        model.form.workItem = "https://linear.app/acme/issue/ENG-7/billing"
        await model.resolve(in: Seed.window)
        #expect(caller.params("session.resolve") == .object([
            "workspace": .string("/src/acme"), "work_item": .string("https://linear.app/acme/issue/ENG-7/billing"),
        ]))
        #expect(model.card == WorkItemCard(
            source: "Linear ENG-7", title: "Billing export", branch: "42-login-redirect", existing: "/w/api-42-login-redirect"
        ))
    }

    @Test func aStaleOrFailedResolveLeavesNoCard() async throws {
        let caller = FakeCaller()
        let model = try await NewSessionFixtures.loaded(caller)
        caller.replies["session.resolve"] = .failure(RPCError(code: "bad_request", message: "not a pull request link"))
        model.form.workItem = "https://github.com/acme/web/issues/4"
        await model.resolve(in: nil)
        #expect(model.card == nil)
        #expect(model.resolveError == "not a pull request link")
        model.form.workItem = ""
        await model.resolve(in: nil)
        #expect(model.resolveError == nil)
        #expect(caller.calls.filter { $0.method == "session.resolve" }.count == 1)
    }

    @Test func theLauncherQueuesSeveralLinearIssues() async throws {
        let caller = FakeCaller()
        let model = try await NewSessionFixtures.loaded(caller)
        caller.replies["launcher.enqueue"] = .success(#"{"queued":["q1","q2"],"rejected":["https://example.com/x"]}"#)
        model.form.launchInput = "https://linear.app/acme/issue/ENG-1/a\nhttps://linear.app/acme/issue/ENG-2/b https://example.com/x"
        await model.enqueue()
        #expect(caller.params("launcher.enqueue") == .object([
            "workspace": .string("/src/acme"), "input": .string(model.form.launchInput), "harness": .string("claude"),
            "model": .string("opus"), "effort": .string("xhigh"),
        ]))
        #expect(model.launchNote == "Queued 2 · not a Linear issue: https://example.com/x")
        #expect(model.form.launchInput == "")
    }

    @Test func theQueueShowsEachItemAndItsStatus() throws {
        let json = """
        [{"ID":"q1","Ref":"ENG-1","URL":"u1","Workspace":"/src/acme","Request":{"Harness":"claude","Model":"opus","Effort":"high"},"Starting":true,"Err":""},
         {"ID":"q2","Ref":"ENG-2","URL":"u2","Workspace":"/src/acme","Request":{"Harness":"codex","Model":"","Effort":""},"Starting":false,"Err":""},
         {"ID":"q3","Ref":"ENG-3","URL":"u3","Workspace":"/src/acme","Request":{"Harness":"claude","Model":"","Effort":""},"Starting":false,"Err":"no such issue"}]
        """
        let queue = try JSONDecoder().decode([JSONValue].self, from: Data(json.utf8))
        #expect(LaunchRow.rows(queue) == [
            LaunchRow(id: "q1", ref: "ENG-1", harness: "CC", status: "starting"),
            LaunchRow(id: "q2", ref: "ENG-2", harness: "CX", status: "queued"),
            LaunchRow(id: "q3", ref: "ENG-3", harness: "CC", status: "failed: no such issue"),
        ])
    }
}
