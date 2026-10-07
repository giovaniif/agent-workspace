import Foundation
import Testing
@testable import AgentwsKit

@MainActor
struct ServerConfigTests {
    static let config = #"""
    {"path":"/u/.agentws/config.toml","values":{"launcher.max_parallel":"4","defaults.codex.model":"gpt-6-sol","push.away_after":"5m","theme.blue":"#000000"}}
    """#

    static let options = #"""
    {"harnesses":[
      {"harness":"claude","name":"Claude Code","tag":"CC","models":["opus","sonnet"],"efforts":["low","high"],"model":"","effort":""},
      {"harness":"codex","name":"Codex","tag":"CX","models":["gpt-6-sol"],"efforts":null,"model":"gpt-6-sol","effort":""}],
     "max_parallel":4}
    """#

    static let devices = #"""
    {"devices":[{"id":"d1","name":"iPhone","created_at":"2026-10-01T10:00:00Z","last_seen":"2026-10-06T09:30:00Z"}]}
    """#

    func caller() -> SettingsCaller {
        let fake = SettingsCaller()
        fake.replies["onboarding.status"] = ServerSettingsTests.status
        fake.replies["workspace.list"] = ServerSettingsTests.workspaces
        fake.replies["config.get"] = Self.config
        fake.replies["session.options"] = Self.options
        fake.replies["device.list"] = Self.devices
        return fake
    }

    @Test func refreshReadsTheServersConfigHarnessesAndPhones() async throws {
        let server = ServerSettings(caller: caller())
        await server.refresh()
        let config = try #require(server.config)
        #expect(config.value("launcher.max_parallel") == "4")
        #expect(config.value("fallback.threshold") == "")
        #expect(config.worktreeLocation == "/u/.agentws/worktrees")
        #expect(server.harnesses.map(\.harness) == ["claude", "codex"])
        #expect(server.devices.map(\.name) == ["iPhone"])
        #expect(server.error == nil)
    }

    @Test func aDaemonWithoutConfigMethodsStillShowsTheRest() async {
        let fake = caller()
        fake.failures["config.get"] = .rpc(RPCError(code: "unknown_method", message: "unknown method config.get"))
        fake.failures["device.list"] = .rpc(RPCError(code: "unknown_method", message: "unknown method device.list"))
        let server = ServerSettings(caller: fake)
        await server.refresh()
        #expect(server.config == nil)
        #expect(server.devices.isEmpty)
        #expect(server.agents != nil)
        #expect(server.workspaces.count == 2)
        #expect(server.error == nil)
    }

    @Test func settingAValueSendsConfigSetAndShowsTheNewValue() async throws {
        let fake = caller()
        fake.replies["config.set"] = #"{"path":"/u/.agentws/config.toml","values":{"launcher.max_parallel":"6"}}"#
        let server = ServerSettings(caller: fake)
        await server.refresh()
        await server.setConfig("launcher.max_parallel", to: "6")
        let sent = try #require(fake.calls.first { $0.method == "config.set" })
        #expect(sent.params == #"{"key":"launcher.max_parallel","value":"6"}"#)
        #expect(server.config?.value("launcher.max_parallel") == "6")
        #expect(server.notice == "Saved launcher.max_parallel in /u/.agentws/config.toml")
    }

    @Test func aRefusedValueShowsTheDaemonsReasonAndKeepsTheOldOne() async {
        let fake = caller()
        fake.failures["config.set"] = .rpc(RPCError(code: "bad_request", message: "fallback.threshold: \"200\" is not a whole number from 0 to 100"))
        let server = ServerSettings(caller: fake)
        await server.refresh()
        await server.setConfig("fallback.threshold", to: "200")
        #expect(server.error == "fallback.threshold: \"200\" is not a whole number from 0 to 100")
        #expect(server.config?.value("fallback.threshold") == "")
    }

    @Test func aPhoneIsRevokedAndANewOneGetsACode() async throws {
        let fake = caller()
        fake.replies["device.revoke"] = "{}"
        fake.replies["pair.code"] = #"{"code":"ABCD2345","expires_at":"2026-10-07T12:05:00Z"}"#
        let server = ServerSettings(caller: fake)
        await server.revokeDevice("d1")
        let revoke = try #require(fake.calls.first { $0.method == "device.revoke" })
        #expect(revoke.params == #"{"id":"d1"}"#)
        await server.pairPhone()
        #expect(server.pairing?.code == "ABCD2345")
        #expect(fake.calls.contains { $0.method == "pair.code" })
    }

    @Test func eachTabListsItsServerSettings() {
        #expect(ConfigFields.workspaces.map(\.key) == ["launcher.max_parallel"])
        #expect(ConfigFields.agents.map(\.key) == ["fallback.threshold"])
        #expect(ConfigFields.notifications.map(\.key) == ["push.away_after"])
        #expect(ConfigFields.theme.count == 15)
        #expect(ConfigFields.theme.first?.key == "theme.text")
        #expect(ConfigFields.defaults(for: "codex").map(\.key) == ["defaults.codex.model", "defaults.codex.effort"])
    }
}
