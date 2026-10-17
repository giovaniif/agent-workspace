import Foundation
import Testing
@testable import CrewKit

let mateListReply = #"""
{"mates":[
 {"name":"helper","prompt":"You help.","agent":"claude","repos":["api"],"owner":"U0OWNER01","enabled":true},
 {"name":"webby","prompt":"Web.","agent":"codex","model":"gpt-5","enabled":false}
],"problems":["/h/config.toml: unknown mate key mate.colour"],"limits":{"hop_limit":5,"turns_per_hour":20,"daily_turns":100,"daily_tokens":0,"stop":false}}
"""#

let approvalsReply = #"""
[{"ID":"a1","SessionID":"s1","Mate":"helper","Requester":"U0GUEST01","Channel":"C1","ThreadTS":"1700.0001","Message":"","Action":{"Kind":"create_pr","Tool":"Bash","Input":"gh pr create"},"At":"2026-10-03T11:00:00Z","State":"pending"}]
"""#

let setupReply = #"""
{"manifest":"{}","url":"https://api.slack.com/apps?new_app=1"}
"""#

@MainActor
struct MatesTests {
    func loaded() async -> (MatesModel, FakeCaller) {
        let caller = FakeCaller()
        caller.replies["mate.list"] = .success(mateListReply)
        caller.replies["approval.list"] = .success(approvalsReply)
        let model = MatesModel(caller: caller)
        await model.load()
        return (model, caller)
    }

    @Test func loadReadsTheDefinitionsAndThePendingApprovals() async {
        let (model, caller) = await loaded()
        #expect(caller.methods() == ["mate.list", "approval.list"])
        #expect(model.definitions.map(\.name) == ["helper", "webby"])
        #expect(model.definition("webby")?.model == "gpt-5")
        #expect(model.problems.count == 1)
        #expect(model.approvals.map(\.title) == ["helper: create pr"])
        #expect(model.approvals.first?.action.input == "gh pr create")
    }

    @Test func noPendingApprovalsIsAnEmptyList() async {
        let caller = FakeCaller()
        caller.replies["mate.list"] = .success(mateListReply)
        caller.replies["approval.list"] = .success("null")
        let model = MatesModel(caller: caller)
        await model.load()
        #expect(model.approvals.isEmpty)
        #expect(model.message == nil)
    }

    @Test func pauseAndResumeCallTheDaemonWithTheName() async {
        let (model, caller) = await loaded()
        caller.replies["mate.pause"] = .success(#"{"name":"helper","prompt":"x","agent":"claude","enabled":false}"#)
        caller.replies["mate.resume"] = .success(#"{"name":"webby","prompt":"x","agent":"codex","enabled":true}"#)
        await model.setPaused("helper", paused: true)
        #expect(caller.calls.last?.method == "mate.pause")
        #expect(caller.calls.last?.params["name"] as? String == "helper")
        await model.setPaused("webby", paused: false)
        #expect(caller.calls.last?.method == "mate.resume")
        #expect(caller.calls.last?.params["name"] as? String == "webby")
    }

    @Test func theKillSwitchStopsAndStartsAllMates() async {
        let (model, caller) = await loaded()
        caller.replies["mate.stop_all"] = .success(#"{"hop_limit":5,"turns_per_hour":20,"daily_turns":100,"daily_tokens":0,"stop":true}"#)
        await model.stopAll(true)
        #expect(caller.calls.last?.method == "mate.stop_all")
        #expect(caller.calls.last?.params["stop"] as? Bool == true)
        await model.stopAll(false)
        #expect(caller.calls.last?.params["stop"] as? Bool == false)
        #expect(model.message == nil)
    }

    @Test func aFailedCallSaysWhy() async {
        let (model, caller) = await loaded()
        caller.replies["mate.pause"] = .failure(RPCError(code: "not_found", message: "no mate \"ghost\""))
        await model.setPaused("ghost", paused: true)
        #expect(model.message?.contains("ghost") == true)
    }

    @Test func saveSendsTheDraftAsAMateAndReloads() async {
        let (model, caller) = await loaded()
        caller.replies["mate.set"] = .success(#"{"name":"scout","prompt":"Scout.","agent":"claude","enabled":true}"#)
        var draft = MateDraft()
        draft.name = " scout "
        draft.prompt = "Scout."
        draft.repos = "api, web ,"
        draft.model = "  "
        let ok = await model.save(draft)
        #expect(ok)
        let sent = caller.calls.first { $0.method == "mate.set" }?.params["mate"] as? [String: Any]
        #expect(sent?["name"] as? String == "scout")
        #expect(sent?["repos"] as? [String] == ["api", "web"])
        #expect(sent?["model"] == nil)
        #expect(sent?["enabled"] as? Bool == true)
        #expect(caller.methods().suffix(2) == ["mate.list", "approval.list"])
    }

    @Test func aDraftKeepsTheOwnerOfTheMate() async {
        let (model, _) = await loaded()
        let draft = MateDraft(model.definition("helper")!)
        #expect(draft.repos == "api")
        #expect(draft.mate.owner == "U0OWNER01")
        #expect(draft.mate == model.definition("helper"))
    }

    @Test func aFailedSetupDoesNotKeepTheLinkOfTheLastMate() async {
        let (model, caller) = await loaded()
        caller.replies["mate.setup"] = .success(setupReply)
        await model.startSetup("helper")
        caller.replies["mate.setup"] = .failure(RPCError(code: "not_found", message: "no mate \"other\""))
        await model.startSetup("other")
        #expect(model.setup == nil)
    }

    @Test func connectSavesTheTokensThenMakesAClaimCode() async {
        let (model, caller) = await loaded()
        caller.replies["mate.setup"] = .success(setupReply)
        caller.replies["mate.set_tokens"] = .success(#"{"name":"helper","has_bot_token":true,"has_app_token":true}"#)
        caller.replies["mate.claim"] = .success(#"{"name":"helper","code":"ABCD2345","expires_at":"2026-10-03T12:10:00Z"}"#)
        await model.startSetup("helper")
        #expect(model.setup?.url == "https://api.slack.com/apps?new_app=1")
        await model.connect("helper", bot: " xoxb-1 ", app: "xapp-1")
        let tokens = caller.calls.first { $0.method == "mate.set_tokens" }?.params
        #expect(tokens?["bot_token"] as? String == "xoxb-1")
        #expect(tokens?["app_token"] as? String == "xapp-1")
        #expect(model.claim?.code == "ABCD2345")
    }

    @Test func decideSendsTheApprovalAndReloads() async {
        let (model, caller) = await loaded()
        caller.replies["approval.decide"] = .success(#"{"ID":"a1","State":"granted"}"#)
        await model.decide("a1", approve: true)
        let sent = caller.calls.first { $0.method == "approval.decide" }?.params
        #expect(sent?["id"] as? String == "a1")
        #expect(sent?["approve"] as? Bool == true)
        #expect(caller.methods().last == "approval.list")
    }

    @Test func aFailedDecisionLoadsTheApprovalsAgainAndKeepsTheError() async {
        let (model, caller) = await loaded()
        caller.replies["approval.decide"] = .failure(RPCError(code: "stale", message: "the approval expired"))
        await model.decide("a1", approve: true)
        #expect(caller.methods().last == "approval.list")
        #expect(model.message == "the approval expired")
    }

    @Test func aRowSaysItsStateItsGuardAndItsCurrentWork() {
        let thread = MateThread(mate: "helper", channel: "C1", threadTS: "1", sessionID: "s1", session: "add login", state: "running")
        let idle = MateThread(mate: "helper", channel: "C1", threadTS: "2", sessionID: "s2", state: "idle")
        let row = MateRow(name: "helper", state: "connected", hold: "rate_limit", work: ["s1"], threads: [thread, idle])
        #expect(row.label == "connected · held by rate limit")
        #expect(row.currentWork.map(\.label) == ["add login"])
        #expect(idle.label == "s2")
        #expect(MateRow(name: "w", state: "no_tokens").label == "no tokens")
        #expect(MateRow(name: "w", state: "paused").paused)
    }
}
