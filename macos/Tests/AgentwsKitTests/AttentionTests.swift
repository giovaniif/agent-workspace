import Foundation
import Testing
@testable import AgentwsKit

@MainActor
final class FakeNotifier: Notifier {
    var posted: [BannerPost] = []
    var withdrawn: [String] = []

    func post(_ banner: BannerPost) { posted.append(banner) }
    func withdraw(_ id: String) { withdrawn.append(id) }
}

@MainActor
final class HeldCaller: Caller {
    var held: [CheckedContinuation<String, Never>] = []

    func call<Params: Encodable & Sendable, Reply: Decodable & Sendable>(_ method: String, params: Params) async throws -> Reply {
        let json = await withCheckedContinuation { held.append($0) }
        return try JSONDecoder().decode(Reply.self, from: Data(json.utf8))
    }
}

let permissionPrompt = #"""
{"id":"p1","text":"Bash: npm test","choices":[{"id":"1","label":"Yes"},{"id":"2","label":"Yes, and don't ask again for npm commands"},{"id":"3","label":"No, and tell Claude what to do differently"}]}
"""#

func noticeLine(_ json: String) throws -> Notice {
    try #require(try JSONDecoder().decode(NoticeLine.self, from: Data(json.utf8)).notice)
}

let permissionNotice = #"""
{"v":1,"id":7,"notice":{"banner":{"Title":"fix login · api@42-retry","Body":"needs permission: Bash: npm test","State":"permission","Sound":"","Group":"s1","Terminal":""}}}
"""#

@MainActor
struct AttentionTests {
    func attention() -> (Attention, FakeCaller, FakeNotifier) {
        let caller = FakeCaller()
        caller.replies["client.viewing"] = .success("{}")
        caller.replies["session.prompt"] = .success(permissionPrompt)
        caller.replies["session.answer"] = .success("{}")
        caller.replies["session.send"] = .success(#"{"id":"q1","queued":false}"#)
        let notifier = FakeNotifier()
        return (Attention(caller: caller, notifier: notifier), caller, notifier)
    }

    @Test func aBannerNoticeDecodesGoFieldNamesAndPostsOneBannerPerSession() throws {
        let (attention, _, notifier) = attention()
        attention.receive(try noticeLine(permissionNotice))
        #expect(notifier.posted == [BannerPost(id: "s1", title: "fix login · api@42-retry", body: "needs permission: Bash: npm test", kind: .permission)])
    }

    @Test func aRemovalWithdrawsTheSessionsBanner() throws {
        let (attention, _, notifier) = attention()
        attention.receive(try noticeLine(#"{"v":1,"id":7,"notice":{"remove":"s2"}}"#))
        #expect(notifier.withdrawn == ["s2"])
        #expect(notifier.posted.isEmpty)
    }

    @Test func waitingBannersOfferReplyAndOthersOnlyOpen() {
        #expect(BannerKind(state: "permission") == .permission)
        #expect(BannerKind(state: "waiting") == .waiting)
        #expect(BannerKind(state: "done") == .other)
    }

    @Test func aSessionEnteringPermissionShowsABannerAndTheCardAndAllowingClearsBoth() async throws {
        let (attention, caller, notifier) = attention()
        attention.receive(try noticeLine(permissionNotice))
        await attention.refreshCard(session: "s1", state: "permission")
        #expect(attention.card?.prompt.text == "Bash: npm test")
        #expect(attention.card?.prompt.choices.map(\.id) == ["1", "2", "3"])
        await attention.allow("s1")
        let answer = try #require(caller.calls.last)
        #expect(answer.method == "session.answer")
        #expect(answer.params["session"] as? String == "s1")
        #expect(answer.params["choice"] as? String == "1")
        #expect(answer.params["prompt"] as? String == "p1")
        #expect(attention.card == nil)
        #expect(notifier.withdrawn == ["s1"])
    }

    @Test func answeringFromTheCardClearsTheCardAndTheBanner() async throws {
        let (attention, caller, notifier) = attention()
        await attention.refreshCard(session: "s1", state: "permission")
        await attention.answer(choice: "3")
        #expect(caller.methods() == ["session.prompt", "session.answer"])
        #expect(caller.calls.last?.params["choice"] as? String == "3")
        #expect(caller.calls.last?.params["prompt"] as? String == "p1")
        #expect(attention.card == nil)
        #expect(notifier.withdrawn == ["s1"])
    }

    @Test func alwaysPicksTheDontAskAgainChoice() async throws {
        let (attention, caller, _) = attention()
        await attention.allow("s1", always: true)
        #expect(caller.methods() == ["session.prompt", "session.answer"])
        #expect(caller.calls.last?.params["choice"] as? String == "2")
    }

    @Test func aStalePromptAnswerSendsNothingAndSaysThePromptIsGone() async throws {
        let (attention, caller, notifier) = attention()
        caller.replies["session.answer"] = .failure(RPCError(code: "stale", message: "the prompt changed"))
        await attention.refreshCard(session: "s1", state: "permission")
        await attention.answer(choice: "1")
        #expect(caller.methods() == ["session.prompt", "session.answer"])
        #expect(attention.card == nil)
        #expect(attention.message == "That prompt is gone; nothing was sent.")
        #expect(notifier.withdrawn == ["s1"])
    }

    @Test func noPromptShowingMeansNothingIsSent() async throws {
        let (attention, caller, _) = attention()
        caller.replies["session.prompt"] = .failure(RPCError(code: "not_found", message: "no dialog"))
        await attention.allow("s1")
        #expect(caller.methods() == ["session.prompt"])
        #expect(attention.message == "That prompt is gone; nothing was sent.")
    }

    @Test func allowingWhileDisconnectedKeepsTheBannerAndTheCard() async {
        let (attention, caller, notifier) = attention()
        await attention.refreshCard(session: "s1", state: "permission")
        caller.replies["session.prompt"] = nil
        await attention.allow("s1")
        #expect(caller.methods() == ["session.prompt", "session.prompt"])
        #expect(attention.message == "Not connected to the daemon.")
        #expect(attention.card?.session == "s1")
        #expect(notifier.withdrawn.isEmpty)
    }

    @Test func anUnparsedDialogShowsNoCard() async {
        let (attention, caller, _) = attention()
        caller.replies["session.prompt"] = .success(#"{"text":"","choices":[],"raw":"Do you want to proceed?"}"#)
        await attention.refreshCard(session: "s1", state: "permission")
        #expect(attention.card == nil)
    }

    @Test func aDroppedConnectionKeepsTheCardButADialogThatIsGoneDropsIt() async {
        let (attention, caller, _) = attention()
        await attention.refreshCard(session: "s1", state: "permission")
        caller.replies["session.prompt"] = nil
        await attention.refreshCard(session: "s1", state: "permission")
        #expect(attention.card?.prompt.id == "p1")
        await attention.refreshCard(session: "s2", state: "permission")
        #expect(attention.card == nil)
        caller.replies["session.prompt"] = .success(permissionPrompt)
        await attention.refreshCard(session: "s1", state: "permission")
        caller.replies["session.prompt"] = .failure(RPCError(code: "not_found", message: "no dialog"))
        await attention.refreshCard(session: "s1", state: "permission")
        #expect(attention.card == nil)
    }

    @Test func leavingPermissionDropsTheCardWithoutAsking() async {
        let (attention, caller, _) = attention()
        await attention.refreshCard(session: "s1", state: "permission")
        await attention.refreshCard(session: "s1", state: "running")
        #expect(attention.card == nil)
        #expect(caller.methods() == ["session.prompt"])
    }

    @Test func showTerminalHidesTheCardUntilAnotherPrompt() async {
        let (attention, caller, _) = attention()
        await attention.refreshCard(session: "s1", state: "permission")
        attention.hideCard()
        #expect(attention.card?.hidden == true)
        await attention.refreshCard(session: "s1", state: "permission")
        #expect(attention.card?.hidden == true)
        caller.replies["session.prompt"] = .success(permissionPrompt.replacingOccurrences(of: "\"p1\"", with: "\"p2\""))
        await attention.refreshCard(session: "s1", state: "permission")
        #expect(attention.card?.hidden == false)
    }

    @Test func noBannerForTheSessionInViewWhileTheAppIsInFront() async throws {
        let (attention, caller, notifier) = attention()
        await attention.view(session: "s1", front: true)
        let viewing = try #require(caller.calls.first)
        #expect(viewing.method == "client.viewing")
        #expect(viewing.params["session"] as? String == "s1")
        #expect(viewing.params["front"] as? Bool == true)
        attention.receive(try noticeLine(permissionNotice))
        #expect(notifier.posted.isEmpty)
        #expect(notifier.withdrawn == ["s1", "s1"])
        await attention.view(session: "s1", front: false)
        attention.receive(try noticeLine(permissionNotice))
        #expect(notifier.posted.map(\.id) == ["s1"])
    }

    @Test func viewingIsReportedOnlyWhenItChanges() async {
        let (attention, caller, _) = attention()
        await attention.view(session: "s1", front: true)
        await attention.view(session: "s1", front: true)
        await attention.view(session: "s2", front: true)
        await attention.view(session: nil, front: false)
        #expect(caller.methods() == ["client.viewing", "client.viewing", "client.viewing"])
        #expect(caller.calls.last?.params["session"] as? String == "")
        #expect(caller.calls.last?.params["front"] as? Bool == false)
    }

    @Test func aWindowGoingToTheBackDoesNotOverrideTheOneNowInFront() async throws {
        let (attention, caller, notifier) = attention()
        await attention.view(session: "s1", front: true, window: "A")
        await attention.view(session: "s2", front: true, window: "B")
        await attention.view(session: "s1", front: false, window: "A")
        #expect(caller.calls.map { $0.params["session"] as? String } == ["s1", "s2"])
        attention.receive(try noticeLine(permissionNotice.replacingOccurrences(of: "\"s1\"", with: "\"s2\"")))
        #expect(notifier.posted.isEmpty)
        await attention.view(session: "s2", front: false, window: "B")
        #expect(caller.calls.last?.params["front"] as? Bool == false)
    }

    @Test func anOlderPromptReplyDoesNotReplaceTheCardOfTheSessionNowShown() async throws {
        let caller = HeldCaller()
        let attention = Attention(caller: caller, notifier: FakeNotifier())
        let first = Task { await attention.refreshCard(session: "s1", state: "permission") }
        while caller.held.isEmpty { await Task.yield() }
        let second = Task { await attention.refreshCard(session: "s2", state: "permission") }
        while caller.held.count < 2 { await Task.yield() }
        caller.held[1].resume(returning: permissionPrompt.replacingOccurrences(of: "\"p1\"", with: "\"p2\""))
        await second.value
        caller.held[0].resume(returning: permissionPrompt)
        await first.value
        #expect(attention.card?.session == "s2")
        #expect(attention.card?.prompt.id == "p2")
    }

    @Test func viewingIsReportedAgainAfterAReconnect() async {
        let (attention, caller, _) = attention()
        await attention.view(session: "s1", front: true)
        await attention.reconnected()
        #expect(caller.methods() == ["client.viewing", "client.viewing"])
    }

    @Test func aViewingReportThatFailedIsSentAgainAfterAReconnect() async {
        let (attention, caller, _) = attention()
        caller.replies["client.viewing"] = nil
        await attention.view(session: "s1", front: true)
        caller.replies["client.viewing"] = .success("{}")
        await attention.reconnected()
        #expect(caller.methods() == ["client.viewing", "client.viewing"])
        #expect(caller.calls.last?.params["session"] as? String == "s1")
    }

    @Test func replySendsTheTextToTheSession() async throws {
        let (attention, caller, notifier) = attention()
        await attention.reply("s2", text: "use the staging db")
        let send = try #require(caller.calls.last)
        #expect(send.method == "session.send")
        #expect(send.params["session"] as? String == "s2")
        #expect(send.params["text"] as? String == "use the staging db")
        #expect(notifier.withdrawn == ["s2"])
    }

    @Test func theMenuListsNeedsYouDoneUnreadAndWorkingWithTheBadge() {
        let menu = AttentionMenu(state: Seed.window, now: Seed.now)
        #expect(menu.needsYou.map(\.id) == ["s1", "s2", "s3"])
        #expect(menu.needsYou.map(\.permission) == [true, false, false])
        #expect(menu.doneUnread.map(\.id) == ["s6"])
        #expect(menu.working.map(\.id) == ["s4", "s5"])
        #expect(menu.meters.map(\.title) == ["Claude 5h", "Claude 7d", "Codex 7d"])
        #expect(menu.badge == "3")
        #expect(menu.title == "3 · 1")
    }

    @Test func anEmptyMenuHasNoBadge() throws {
        var state = Seed.window
        state.sessions = []
        let menu = AttentionMenu(state: state, now: Seed.now)
        #expect(menu.badge == nil)
        #expect(menu.title == "")
        #expect(AttentionMenu(state: nil, now: Seed.now).badge == nil)
    }

    @Test(.timeLimit(.minutes(1))) func theStreamSubscribesAndDeliversNotices() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", """
        IFS= read -r line
        printf '%s' "$line" > "\(bin.path("request"))"
        printf '{"v":1,"id":1,"result":{}}\\n'
        printf '%s\\n' '\(permissionNotice.trimmingCharacters(in: .whitespacesAndNewlines))'
        printf '{"v":1,"id":1,"notice":{"remove":"s1"}}\\n'
        cat > /dev/null
        """)
        var notices: [Notice] = []
        let stream = NoticeStream(endpoint: .local(binary: agentws), build: "v1", environment: bin.environment) { notices.append($0) }
        stream.start()
        while notices.count < 2 { try await Task.sleep(for: .milliseconds(20)) }
        stream.stop()
        #expect(notices.first?.banner?.group == "s1")
        #expect(notices.last?.remove == "s1")
        let request = try #require(try JSONSerialization.jsonObject(with: Data(bin.read("request").utf8)) as? [String: Any])
        #expect(request["method"] as? String == "notify.stream")
        #expect(request["build"] as? String == "v1")
    }

    @Test func aBridgeAgentIsFoundWithItsHostAndRemovedThroughSetup() throws {
        let bin = try FakeBin()
        let plist: [String: Any] = [
            "Label": "dev.agentws.bridge.box",
            "ProgramArguments": ["/usr/local/bin/agentws", "notify", "bridge", "--remote-bin", "~/bin/agentws", "box"],
        ]
        let data = try PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0)
        try data.write(to: bin.dir.appendingPathComponent("dev.agentws.bridge.box.plist"))
        try "x".write(to: bin.dir.appendingPathComponent("dev.agentws.serve.plist"), atomically: true, encoding: .utf8)
        let agents = BridgeAgents.find(in: bin.dir)
        #expect(agents == [BridgeAgent(label: "dev.agentws.bridge.box", host: "box", remoteBinary: "~/bin/agentws")])
        #expect(agents.first?.removeArgv(binary: "/a/agentws") == ["/a/agentws", "setup", "bridge", "--remote-bin", "~/bin/agentws", "--remove", "box"])
        #expect(BridgeAgent(label: "l", host: "h", remoteBinary: nil).removeArgv(binary: "agentws") == ["agentws", "setup", "bridge", "--remove", "h"])
    }

    @Test func noLaunchAgentsFolderMeansNoBridge() {
        #expect(BridgeAgents.find(in: URL(fileURLWithPath: "/nonexistent-\(UUID().uuidString)")).isEmpty)
    }
}
