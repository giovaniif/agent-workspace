import Foundation
import Testing
import AgentwsKit

let automationsReply = #"""
{"automations":[
 {"name":"fix-ci","agent":"claude","target":"new-worktree","prompt":"Fix the CI of {{work_item}}.","on":["pr.ci_failed"],"enabled":true,"source":"/h/config.toml","trusted":true},
 {"name":"deps","agent":"codex","prompt":"Update the deps.","schedule":"weekly mon 09:00","enabled":false,"source":"/h/config.toml","trusted":true},
 {"name":"audit","agent":"claude","prompt":"Audit the api for secrets.","enabled":true,"source":"/w/api/.agentws.toml"}
],"problems":["/h/config.toml: automation \"x\": agent must be one of [claude codex omp]"]}
"""#

let runsReply = #"""
{"runs":[
 {"id":"r3","automation":"fix-ci","trigger":"event","start":"2026-10-13T10:05:00Z","end":"2026-10-13T10:05:01Z","outcome":"succeeded","session_id":"s3","work_item":"https://github.com/o/api/pull/42"},
 {"id":"r2","automation":"deps","trigger":"schedule","start":"2026-10-13T09:00:00.123456Z","end":"2026-10-13T09:00:00Z","outcome":"skipped","reason":"the last run is still live"},
 {"id":"r1","automation":"fix-ci","trigger":"manual","context":"only lint","start":"2026-10-12T08:00:00Z","end":"2026-10-12T08:00:01Z","outcome":"failed","reason":"no harness"}
]}
"""#

struct AutomationsTests {
    func decoded() throws -> (AutomationList, [AutomationRun]) {
        let list = try JSONDecoder().decode(AutomationList.self, from: Data(automationsReply.utf8))
        let runs = try JSONDecoder().decode(AutomationRuns.self, from: Data(runsReply.utf8)).runs
        return (list, runs)
    }

    @Test func automationsAndRunsDecodeTheDaemonsFieldNames() throws {
        let (list, runs) = try decoded()
        #expect(list.automations.count == 3)
        #expect(list.problems.count == 1)
        #expect(list.automations[0].on == ["pr.ci_failed"])
        #expect(list.automations[0].trigger == "on pr.ci_failed")
        #expect(list.automations[1].trigger == "weekly mon 09:00")
        #expect(list.automations[2].trigger == "manual")
        #expect(list.automations[2].targetLabel == "new-worktree")
        #expect(list.automations[2].repoFile == "/w/api/.agentws.toml")
        #expect(list.automations[0].repoFile == nil)
        #expect(runs[0].sessionID == "s3")
        #expect(runs[0].workItem == "https://github.com/o/api/pull/42")
        #expect(runs[1].started != nil)
        #expect(runs[2].context == "only lint")
    }

    @Test func rowsCarryTheLastRunAndSortByNameOrLastRun() throws {
        let (list, runs) = try decoded()
        let byName = AutomationTable.rows(list.automations, runs: runs, query: AutomationQuery())
        #expect(byName.map(\.automation.name) == ["audit", "deps", "fix-ci"])
        #expect(byName[0].lastOutcome == "never run")
        #expect(byName[2].lastOutcome == "succeeded")
        let byRun = AutomationTable.rows(list.automations, runs: runs, query: AutomationQuery(sort: .lastRun))
        #expect(byRun.map(\.automation.name) == ["fix-ci", "deps", "audit"])
    }

    @Test func searchAndFiltersNarrowTheRows() throws {
        let (list, runs) = try decoded()
        let names = { (q: AutomationQuery) in AutomationTable.rows(list.automations, runs: runs, query: q).map(\.automation.name) }
        #expect(names(AutomationQuery(search: "SECRETS")) == ["audit"])
        #expect(names(AutomationQuery(search: "fix")) == ["fix-ci"])
        #expect(names(AutomationQuery(enabled: false)) == ["deps"])
        #expect(names(AutomationQuery(enabled: true)) == ["audit", "fix-ci"])
        #expect(names(AutomationQuery(outcome: "skipped")) == ["deps"])
        #expect(names(AutomationQuery(agent: "codex")) == ["deps"])
    }

    @Test func arrowsMoveTheSelectionAndStopAtTheEnds() throws {
        let (list, runs) = try decoded()
        let rows = AutomationTable.rows(list.automations, runs: runs, query: AutomationQuery())
        #expect(AutomationTable.move(nil, by: 1, in: rows) == rows[0].id)
        #expect(AutomationTable.move(rows[0].id, by: 1, in: rows) == rows[1].id)
        #expect(AutomationTable.move(rows[2].id, by: 1, in: rows) == rows[2].id)
        #expect(AutomationTable.move(rows[0].id, by: -1, in: rows) == rows[0].id)
        #expect(AutomationTable.move("gone", by: 1, in: rows) == rows[0].id)
        #expect(AutomationTable.move(nil, by: 1, in: []) == nil)
    }

    @Test func runsOfOneAutomationAreNewestFirst() throws {
        let (_, runs) = try decoded()
        #expect(AutomationTable.runs(of: "fix-ci", in: runs).map(\.id) == ["r3", "r1"])
    }

    @MainActor
    @Test func runSendsTheContextAndRecordsTheRun() async throws {
        let caller = FakeCaller()
        caller.replies["automation.list"] = .success(automationsReply)
        caller.replies["automation.runs"] = .success(runsReply)
        caller.replies["automation.run"] = .success(#"{"run":{"id":"r4","automation":"fix-ci","trigger":"manual","start":"2026-10-13T11:00:00Z","end":"2026-10-13T11:00:01Z","outcome":"succeeded","session_id":"s4"},"session":{}}"#)
        let model = AutomationsModel(caller: caller)
        await model.load()
        #expect(model.list.count == 3)
        let run = await model.run("fix-ci", context: "only the lint job", workItem: "")
        #expect(run?.sessionID == "s4")
        #expect(caller.calls.last?.method == "automation.run")
        #expect(caller.calls.last?.params["name"] as? String == "fix-ci")
        #expect(caller.calls.last?.params["context"] as? String == "only the lint job")
        #expect(model.runs.first?.id == "r4")
        #expect(model.message == nil)
    }

    @MainActor
    @Test func aSkippedRunSaysWhy() async throws {
        let caller = FakeCaller()
        caller.replies["automation.run"] = .success(#"{"run":{"id":"r5","automation":"deps","trigger":"manual","start":"2026-10-13T11:00:00Z","end":"2026-10-13T11:00:01Z","outcome":"skipped","reason":"precheck failed"},"session":{}}"#)
        let model = AutomationsModel(caller: caller)
        _ = await model.rerun("r2")
        #expect(caller.calls.last?.params["rerun"] as? String == "r2")
        #expect(model.message == "deps: skipped (precheck failed)")
    }

    @MainActor
    @Test func saveWritesTheGlobalConfigAndReloads() async throws {
        let caller = FakeCaller()
        caller.replies["automation.save"] = .success(#"{"name":"new","agent":"claude","prompt":"Do it.","enabled":true,"source":"/h/config.toml"}"#)
        caller.replies["automation.list"] = .success(automationsReply)
        caller.replies["automation.runs"] = .success(runsReply)
        let model = AutomationsModel(caller: caller)
        var draft = AutomationDraft()
        draft.name = "new"
        draft.prompt = "Do it."
        draft.on = "pr.opened, pr.merged"
        let ok = await model.save(draft)
        #expect(ok)
        let sent = caller.calls.first?.params["automation"] as? [String: Any]
        #expect(sent?["name"] as? String == "new")
        #expect(sent?["agent"] as? String == "claude")
        #expect(sent?["on"] as? [String] == ["pr.opened", "pr.merged"])
        #expect(sent?["enabled"] as? Bool == true)
        #expect(caller.methods() == ["automation.save", "automation.list", "automation.runs"])
    }

    @Test func aDraftStartsFromAnAutomation() throws {
        let (list, _) = try decoded()
        let draft = AutomationDraft(list.automations[0])
        #expect(draft.name == "fix-ci")
        #expect(draft.on == "pr.ci_failed")
        #expect(draft.target == "new-worktree")
    }
}
