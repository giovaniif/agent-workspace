import Foundation
import Testing
@testable import CrewKit

@MainActor
struct HeadlessViewTests {
    func make(_ reply: Result<String, RPCError>?) -> (HeadlessView, FakeCaller) {
        let caller = FakeCaller()
        caller.replies["browser.headless.view"] = reply
        return (HeadlessView(caller: caller), caller)
    }

    @Test func aRunningHeadlessTabGivesAFrame() async throws {
        let (view, caller) = make(.success(#"{"live":true,"png":"iVBORw0KGgo=","width":1280,"height":800}"#))
        await view.refresh(session: "s1")
        #expect(caller.methods() == ["browser.headless.view"])
        #expect(caller.calls.last?.params["session"] as? String == "s1")
        let frame = try #require(view.frame)
        #expect(frame.session == "s1")
        #expect(frame.png == Data([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]))
        #expect(frame.width == 1280)
        #expect(frame.height == 800)
    }

    @Test func noHeadlessTabGivesNoFrame() async {
        let (view, _) = make(.success(#"{"live":false}"#))
        await view.refresh(session: "s1")
        #expect(view.frame == nil)
    }

    @Test func theFrameGoesWhenTheTabStops() async {
        let (view, caller) = make(.success(#"{"live":true,"png":"iVBORw0KGgo=","width":1,"height":1}"#))
        await view.refresh(session: "s1")
        #expect(view.frame != nil)
        caller.replies["browser.headless.view"] = .success(#"{"live":false}"#)
        await view.refresh(session: "s1")
        #expect(view.frame == nil)
    }

    @Test func aFailedCallOrAnOlderServerGivesNoFrame() async {
        let (view, caller) = make(.success(#"{"live":true,"png":"iVBORw0KGgo=","width":1,"height":1}"#))
        await view.refresh(session: "s1")
        caller.replies["browser.headless.view"] = .failure(RPCError(code: "unknown_method", message: "unknown method"))
        await view.refresh(session: "s1")
        #expect(view.frame == nil)
        caller.replies["browser.headless.view"] = nil
        await view.refresh(session: "s1")
        #expect(view.frame == nil)
    }

    @Test func aFrameOfADifferentSessionIsNotShown() async {
        let (view, _) = make(.success(#"{"live":true,"png":"iVBORw0KGgo=","width":1,"height":1}"#))
        await view.refresh(session: "s1")
        #expect(view.frame(for: "s1") != nil)
        #expect(view.frame(for: "s2") == nil)
    }
}
