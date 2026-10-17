import Foundation
import Testing
@testable import CrewKit

@MainActor
struct ServerProfileTests {
    let cookies = [
        CookieRecord(name: "sid", value: "abc123", domain: ".example.com", path: "/", expires: Date(timeIntervalSince1970: 1893456000), secure: true, httpOnly: true),
        CookieRecord(name: "token", value: "xyz", domain: "app.example.com", path: "/api", expires: nil, secure: false, httpOnly: false),
        CookieRecord(name: "pref", value: "dark", domain: "other.dev", path: "/", expires: nil, secure: false, httpOnly: false),
        CookieRecord(name: "", value: "nameless", domain: "example.com", path: "/", expires: nil, secure: false, httpOnly: false),
    ]

    func make(reply: Result<String, RPCError>? = .success(#"{"imported":2,"total":5}"#)) -> (ServerProfileSend, FakeCaller) {
        let caller = FakeCaller()
        caller.replies["browser.cookies.import"] = reply
        return (ServerProfileSend(caller: caller), caller)
    }

    @Test func onlyTheCookiesOfTheChosenSitesGoToTheServer() {
        let chosen = ServerProfile.cookies(cookies, domains: ["example.com"])
        #expect(chosen.map(\.name) == ["sid", "token"])
        #expect(chosen[0] == ServerCookie(name: "sid", value: "abc123", domain: ".example.com", path: "/", expires: 1893456000, secure: true, httpOnly: true))
        #expect(chosen[1].expires == nil)
        #expect(ServerProfile.cookies(cookies, domains: []).isEmpty)
    }

    @Test func theSendCallsBrowserCookiesImportWithTheChosenCookies() async throws {
        let (send, caller) = make()
        let sent = await send.send(cookies, domains: ["example.com"])
        #expect(sent)
        #expect(caller.methods() == ["browser.cookies.import"])
        let list = try #require(caller.calls.last?.params["cookies"] as? [[String: Any]])
        #expect(list.count == 2)
        #expect(list[0]["name"] as? String == "sid")
        #expect(list[0]["value"] as? String == "abc123")
        #expect(list[0]["domain"] as? String == ".example.com")
        #expect(list[0]["path"] as? String == "/")
        #expect(list[0]["expires"] as? Double == 1893456000)
        #expect(list[0]["secure"] as? Bool == true)
        #expect(list[0]["httpOnly"] as? Bool == true)
        #expect(list[1]["expires"] == nil)
        #expect(send.status == "Sent 2 cookies to the server. It keeps 5.")
    }

    @Test func noChosenSiteSendsNothing() async {
        let (send, caller) = make()
        let sent = await send.send(cookies, domains: ["nowhere.test"])
        #expect(!sent)
        #expect(caller.calls.isEmpty)
        #expect(send.status == "Select a site with cookies.")
    }

    @Test func aRefusalShowsTheMessageOfTheDaemon() async {
        let (send, _) = make(reply: .failure(RPCError(code: "failed", message: "disk full")))
        let sent = await send.send(cookies, domains: ["other.dev"])
        #expect(!sent)
        #expect(send.status == "disk full")
        #expect(!send.sending)
    }

    @Test func anOlderServerWithoutTheMethodSaysSo() async {
        let (send, _) = make(reply: .failure(RPCError(code: "unknown_method", message: "unknown method")))
        _ = await send.send(cookies, domains: ["other.dev"])
        #expect(send.status == "This server has no headless browser. Update the server.")
    }
}
