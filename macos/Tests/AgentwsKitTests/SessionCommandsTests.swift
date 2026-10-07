import Foundation
import Testing
@testable import AgentwsKit

let optionsReply = #"""
{"harnesses":[{"harness":"claude","name":"Claude Code","tag":"CC","models":["opus","sonnet"],"efforts":["low","high"],"model":"opus","effort":"high"},{"harness":"codex","name":"Codex","tag":"CX","models":["gpt-5"],"efforts":["medium"],"model":"gpt-5","effort":"medium"}],"max_parallel":3}
"""#

@MainActor
struct SessionCommandsTests {
    func commands() -> (SessionCommands, FakeCaller) {
        let caller = FakeCaller()
        for method in ["session.rename", "session.mute", "session.switch", "session.end", "session.resume"] {
            caller.replies[method] = .success("{}")
        }
        caller.replies["ports.kill"] = .success(#"{"killed":[1]}"#)
        caller.replies["session.options"] = .success(optionsReply)
        return (SessionCommands(caller: caller), caller)
    }

    func session(_ id: String) throws -> Session {
        try #require(Seed.window.sessions.first { $0.id == id })
    }

    @Test func renamePinsTheTrimmedName() async throws {
        let (commands, caller) = commands()
        await commands.rename("s1", to: "  login fix ")
        let call = try #require(caller.calls.last)
        #expect(call.method == "session.rename")
        #expect(call.params["id"] as? String == "s1")
        #expect(call.params["name"] as? String == "login fix")
    }

    @Test func anEmptyNameRenamesNothing() async {
        let (commands, caller) = commands()
        await commands.rename("s1", to: "  ")
        #expect(caller.calls.isEmpty)
    }

    @Test func muteFlipsTheSessionsMutedFlag() async throws {
        let (commands, caller) = commands()
        await commands.toggleMute(try session("s1"))
        await commands.toggleMute(try session("s7"))
        #expect(caller.calls.map { $0.params["muted"] as? Bool } == [true, false])
        #expect(caller.calls.map(\.method) == ["session.mute", "session.mute"])
    }

    @Test func modelAndEffortChoicesComeFromTheSessionsHarness() async throws {
        let (commands, _) = commands()
        #expect(await commands.choices(for: try session("s1"), kind: .model) == ["opus", "sonnet"])
        #expect(await commands.choices(for: try session("s2"), kind: .effort) == ["medium"])
    }

    @Test func switchingSendsTheKindAndValue() async throws {
        let (commands, caller) = commands()
        await commands.switchTo("s1", kind: .effort, value: "low")
        let call = try #require(caller.calls.last)
        #expect(call.method == "session.switch")
        #expect(call.params["session_id"] as? String == "s1")
        #expect(call.params["kind"] as? String == "effort")
        #expect(call.params["value"] as? String == "low")
    }

    @Test func endAndResumeNameTheSession() async {
        let (commands, caller) = commands()
        await commands.end("s1")
        await commands.resume("s10")
        #expect(caller.calls.map(\.method) == ["session.end", "session.resume"])
        #expect(caller.calls.map { $0.params["id"] as? String } == ["s1", "s10"])
    }

    @Test func killDevServersSendsTheProcessGroupsOfTheSessionsWorktrees() async throws {
        let (commands, caller) = commands()
        #expect(SessionCommands.devServers(of: "s2", in: Seed.window) == [1])
        await commands.killDevServers("s2", state: Seed.window)
        let call = try #require(caller.calls.last)
        #expect(call.method == "ports.kill")
        #expect(call.params["pgids"] as? [Int] == [1])
    }

    @Test func aSessionWithoutDevServersKillsNothingAndSaysSo() async {
        let (commands, caller) = commands()
        await commands.killDevServers("s3", state: Seed.window)
        #expect(caller.calls.isEmpty)
        #expect(commands.message == "No dev servers are running in this session's worktrees.")
    }

    @Test func aFailedCallShowsTheDaemonsMessage() async {
        let (commands, caller) = commands()
        caller.replies["session.end"] = .failure(RPCError(code: "failed", message: "tmux is gone"))
        await commands.end("s1")
        #expect(commands.message == "tmux is gone")
    }
}
