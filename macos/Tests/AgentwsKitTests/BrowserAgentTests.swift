import Foundation
import Testing
@testable import AgentwsKit

struct BrowserAgentTests {
    private func call(_ command: String, _ args: [String: JSONValue] = [:]) -> BrowserCall {
        BrowserCall(call: 7, session: "s1", command: command, args: args)
    }

    @Test func aCallDecodesWithTheGoFieldNamesAndNullArgs() throws {
        let line = #"{"v":1,"id":3,"browser":{"call":9,"session":"s1","command":"snapshot","args":null}}"#
        let call = try #require(BrowserCall.decode(line: Data(line.utf8)))
        #expect(call == BrowserCall(call: 9, session: "s1", command: "snapshot", args: [:]))
        let withArgs = #"{"v":1,"id":3,"browser":{"call":2,"session":"s1","command":"fill","args":{"ref":"@e2","text":"hi"}}}"#
        #expect(BrowserCall.decode(line: Data(withArgs.utf8))?.args == ["ref": .string("@e2"), "text": .string("hi")])
        #expect(BrowserCall.decode(line: Data(#"{"v":1,"id":3,"result":{"attached":true}}"#.utf8)) == nil)
    }

    @Test func eachCommandMapsToOneStep() throws {
        #expect(try BrowserStep.of(call("goto", ["url": .string("http://localhost:3000/")])) == .goto("http://localhost:3000/"))
        #expect(try BrowserStep.of(call("snapshot")) == .script(BrowserScript.snapshot, [:]))
        #expect(try BrowserStep.of(call("click", ["ref": .string("@e3")])) == .script(BrowserScript.click, ["ref": "e3"]))
        #expect(try BrowserStep.of(call("fill", ["ref": .string("@e2"), "text": .string("a b")])) == .script(BrowserScript.fill, ["ref": "e2", "text": "a b"]))
        #expect(try BrowserStep.of(call("type", ["text": .string("hey")])) == .script(BrowserScript.type, ["text": "hey"]))
        #expect(try BrowserStep.of(call("press", ["key": .string("Enter")])) == .script(BrowserScript.press, ["key": "Enter"]))
        #expect(try BrowserStep.of(call("eval", ["js": .string("1+1")])) == .script(BrowserScript.eval, ["js": "1+1"]))
        #expect(try BrowserStep.of(call("wait", ["ms": .number(250)])) == .sleep(250))
        #expect(try BrowserStep.of(call("wait", ["ref": .string("@e4")])) == .until(BrowserScript.hasRef, ["ref": "e4"]))
        #expect(try BrowserStep.of(call("wait", ["text": .string("Done")])) == .until(BrowserScript.hasText, ["text": "Done"]))
        #expect(try BrowserStep.of(call("screenshot")) == .screenshot)
        #expect(try BrowserStep.of(call("tabs")) == .tabs)
        #expect(try BrowserStep.of(call("cookies")) == .cookies(nil))
        #expect(try BrowserStep.of(call("cookies", ["domain": .string("example.com")])) == .cookies("example.com"))
        #expect(try BrowserStep.of(call("set-device", ["device": .string("iphone")])) == .device(.iphone))
    }

    @Test func aBadCallIsRefusedWithAReason() {
        #expect(throws: BrowserRefusal.unknown("fly")) { try BrowserStep.of(call("fly")) }
        #expect(throws: BrowserRefusal.missing("url")) { try BrowserStep.of(call("goto")) }
        #expect(throws: BrowserRefusal.missing("ref")) { try BrowserStep.of(call("click", ["ref": .string("e1")])) }
        #expect(throws: BrowserRefusal.missing("device")) { try BrowserStep.of(call("set-device", ["device": .string("watch")])) }
        #expect(BrowserRefusal.unknown("fly").message == "unknown browser command fly")
        #expect(BrowserRefusal.missing("url").message == "the call has no valid url")
    }

    @Test func scriptsTakeTheirValuesAsArgumentsNotAsSource() {
        #expect(BrowserScript.fill.contains("text"))
        #expect(BrowserScript.press.contains("key"))
        #expect(BrowserScript.hasText.contains("text"))
        #expect(BrowserScript.click.contains("data-agentws-ref"))
        #expect(BrowserScript.snapshot.contains("data-agentws-ref"))
        #expect(BrowserScript.eval.contains("js"))
    }

    @Test func aReplyCarriesTheResultOrTheError() throws {
        let ok = try JSONSerialization.jsonObject(with: BrowserReply(call: 4, result: .string("clicked")).request(id: 11, build: "b1")) as? [String: Any]
        #expect(ok?["method"] as? String == "browser.reply")
        #expect(ok?["build"] as? String == "b1")
        let params = ok?["params"] as? [String: Any]
        #expect(params?["call"] as? Int == 4)
        #expect(params?["result"] as? String == "clicked")
        #expect(params?["error"] == nil)
        let failed = try JSONSerialization.jsonObject(with: BrowserReply(call: 5, error: "no such ref").request(id: 12, build: "b1")) as? [String: Any]
        let failedParams = failed?["params"] as? [String: Any]
        #expect(failedParams?["error"] as? String == "no such ref")
        #expect(failedParams?["result"] == nil)
    }

    @Test func thePolicyAllowsTheServerHostAndPublicHostsOnly() {
        let policy = BrowserPolicy(serverHost: "devbox")
        #expect(policy.refusal(url: "http://devbox:3000/x", addresses: []) == nil)
        #expect(policy.refusal(url: "https://example.com/", addresses: ["93.184.216.34", "2606:2800:220:1::1"]) == nil)
        #expect(policy.refusal(url: "file:///etc/passwd", addresses: []) == "only http and https are allowed")
        #expect(policy.refusal(url: "http://router.local/", addresses: ["192.168.1.1"]) == "router.local is on a private network")
        #expect(policy.refusal(url: "http://localhost:8080/", addresses: ["127.0.0.1"]) == "localhost is on a private network")
        #expect(policy.refusal(url: "http://nowhere.example/", addresses: []) == "nowhere.example does not resolve")
        #expect(policy.refusal(url: "about:blank", addresses: []) == nil)
        #expect(BrowserPolicy(serverHost: "localhost").refusal(url: "http://localhost:5173/", addresses: []) == nil)
    }

    @Test func onlyPublicAddressesArePublic() {
        for address in ["127.0.0.1", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.0.10", "169.254.1.1", "100.64.0.1", "100.127.255.255", "0.0.0.0", "224.0.0.1", "::1", "fe80::1", "fc00::1", "fd12::1", "::", "ff02::1", "::ffff:192.168.1.1"] {
            #expect(!BrowserPolicy.isPublic(address), "\(address)")
        }
        for address in ["8.8.8.8", "172.32.0.1", "100.128.0.1", "2606:4700::1111", "::ffff:8.8.8.8"] {
            #expect(BrowserPolicy.isPublic(address), "\(address)")
        }
    }

    @Test(.timeLimit(.minutes(1))) func theLinkAttachesRunsEachCallAndReplies() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", """
        IFS= read -r attach
        printf '%s\\n' "$attach" > "\(bin.path("attach"))"
        printf '{"v":1,"id":1,"result":{"attached":true}}\\n'
        printf '{"v":1,"id":1,"browser":{"call":5,"session":"s1","command":"click","args":{"ref":"@e1"}}}\\n'
        IFS= read -r reply
        printf '%s\\n' "$reply" > "\(bin.path("reply"))"
        """)
        let seen = CallLog()
        let link = BrowserLink(endpoint: .local(binary: agentws), build: "b2", environment: bin.environment) { call in
            await seen.add(call)
            return .result(.string("clicked"))
        }
        let outcome = await link.runOnce()
        #expect(outcome == .ended)
        #expect(await seen.calls.map(\.command) == ["click"])
        let attach = try #require(try JSONSerialization.jsonObject(with: Data(bin.read("attach").utf8)) as? [String: Any])
        #expect(attach["method"] as? String == "browser.attach")
        #expect(attach["build"] as? String == "b2")
        let reply = try #require(try JSONSerialization.jsonObject(with: Data(bin.read("reply").utf8)) as? [String: Any])
        #expect(reply["method"] as? String == "browser.reply")
        #expect((reply["params"] as? [String: Any])?["call"] as? Int == 5)
    }

    @Test(.timeLimit(.minutes(1))) func aBuildMismatchStopsTheLink() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", """
        IFS= read -r attach
        printf '{"v":1,"id":1,"error":{"code":"version_mismatch","message":"restart"}}\\n'
        cat > /dev/null
        """)
        let link = BrowserLink(endpoint: .local(binary: agentws), build: "b2", environment: bin.environment) { _ in .result(.null) }
        #expect(await link.runOnce() == .stop)
    }
}

actor CallLog {
    var calls: [BrowserCall] = []
    func add(_ call: BrowserCall) { calls.append(call) }
}
