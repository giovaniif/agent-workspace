import Foundation
import Testing
@testable import AgentwsKit

func bannerNotice(state: String, session: String = "s1") -> String {
    #"{"v":1,"id":7,"notice":{"banner":{"Title":"fix login","Body":"b","State":""# + state + #"","Sound":"","Group":""# + session + #"","Terminal":""}}}"#
}

@MainActor
struct NotificationPrefsTests {
    func attention(_ prefs: NotificationSettings, muted: Set<String> = []) -> (Attention, FakeNotifier) {
        let caller = FakeCaller()
        caller.replies["client.viewing"] = .success("{}")
        let notifier = FakeNotifier()
        let attention = Attention(caller: caller, notifier: notifier)
        attention.preferences = prefs
        attention.isMuted = { muted.contains($0) }
        return (attention, notifier)
    }

    @Test func anEventWithItsBannerOffPostsNothing() throws {
        var prefs = NotificationSettings()
        prefs.waiting.banner = false
        let (attention, notifier) = attention(prefs)
        attention.receive(try noticeLine(bannerNotice(state: "waiting")))
        attention.receive(try noticeLine(bannerNotice(state: "done", session: "s2")))
        #expect(notifier.posted.map(\.id) == ["s2"])
    }

    @Test func aSuppressedBannerWithdrawsTheSessionsEarlierOne() throws {
        var prefs = NotificationSettings()
        prefs.waiting.banner = false
        let (attention, notifier) = attention(prefs)
        attention.receive(try noticeLine(bannerNotice(state: "permission")))
        attention.receive(try noticeLine(bannerNotice(state: "waiting")))
        #expect(notifier.posted.map(\.id) == ["s1"])
        #expect(notifier.withdrawn == ["s1"])
    }

    @Test func eachEventPlaysASoundOnlyWhenItsSoundIsOn() throws {
        var prefs = NotificationSettings()
        prefs.permission.sound = false
        prefs.done.sound = true
        let (attention, notifier) = attention(prefs)
        attention.receive(try noticeLine(bannerNotice(state: "permission", session: "s1")))
        attention.receive(try noticeLine(bannerNotice(state: "done", session: "s2")))
        attention.receive(try noticeLine(bannerNotice(state: "waiting", session: "s3")))
        #expect(notifier.posted.map(\.sound) == [false, true, false])
    }

    @Test func anyOtherStateFollowsTheLimitOrErrorEvent() throws {
        var prefs = NotificationSettings()
        prefs.limitOrError = EventAlert(banner: false, sound: false)
        let (attention, notifier) = attention(prefs)
        attention.receive(try noticeLine(bannerNotice(state: "limit")))
        #expect(notifier.posted.isEmpty)
    }

    @Test func theSessionInViewStillBannersWhenSkippingIsOff() async throws {
        var prefs = NotificationSettings()
        prefs.skipSessionInView = false
        let (attention, notifier) = attention(prefs)
        await attention.view(session: "s1", front: true)
        attention.receive(try noticeLine(bannerNotice(state: "waiting")))
        #expect(notifier.posted.map(\.id) == ["s1"])
    }

    @Test func theSessionInViewIsSkippedByDefault() async throws {
        let (attention, notifier) = attention(NotificationSettings())
        await attention.view(session: "s1", front: true)
        attention.receive(try noticeLine(bannerNotice(state: "waiting")))
        #expect(notifier.posted.isEmpty)
    }

    @Test func aMutedSessionBannersOnlyWhenMutedSessionsNotify() throws {
        let (quiet, quietNotifier) = attention(NotificationSettings(), muted: ["s1"])
        quiet.receive(try noticeLine(bannerNotice(state: "waiting")))
        #expect(quietNotifier.posted.isEmpty)
        var prefs = NotificationSettings()
        prefs.notifyMuted = true
        let (loud, loudNotifier) = attention(prefs, muted: ["s1"])
        loud.receive(try noticeLine(bannerNotice(state: "waiting")))
        #expect(loudNotifier.posted.map(\.id) == ["s1"])
    }
}
