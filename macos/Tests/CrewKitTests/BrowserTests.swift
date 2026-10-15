import Foundation
import Testing
@testable import CrewKit

@MainActor
struct BrowserTests {
    func freshDefaults() -> (UserDefaults, String) {
        let suite = "crew-browser-tests-\(UUID().uuidString)"
        return (UserDefaults(suiteName: suite)!, suite)
    }

    @Test(arguments: [
        ("3000", "devbox", "http://devbox:3000/"),
        (":5173", "devbox", "http://devbox:5173/"),
        ("localhost:8080/login", "devbox", "http://devbox:8080/login"),
        ("127.0.0.1:4000", "localhost", "http://localhost:4000/"),
        ("example.com", "devbox", "https://example.com"),
        ("https://example.com/a?b=1", "devbox", "https://example.com/a?b=1"),
        ("http://devbox:3000/", "devbox", "http://devbox:3000/"),
        ("  docs.dev/x  ", "devbox", "https://docs.dev/x"),
    ])
    func anAddressBecomesAURL(input: String, host: String, url: String) {
        #expect(BrowserAddress.url(from: input, host: host) == url)
    }

    @Test(arguments: ["", "   ", "not a url", "javascript:alert(1)", "file:///etc/passwd", "localhost:99999", "localhost:0", "localhost:abc"])
    func aBadAddressIsRefused(input: String) {
        #expect(BrowserAddress.url(from: input, host: "devbox") == nil)
    }

    @Test func theHostOfAServerIsWhereItsDevServersListen() {
        #expect(BrowserHost.of(.thisMac) == "localhost")
        #expect(BrowserHost.of(.ssh(host: "devbox")) == "devbox")
        #expect(BrowserHost.of(.ssh(host: "me@host.tail1234.ts.net")) == "host.tail1234.ts.net")
    }

    @Test func theKnownPortsOfASessionAreQuickLinks() {
        let links = BrowserLinks.ports(session: "s2", state: Seed.window, host: "devbox")
        #expect(links.map(\.url) == ["http://devbox:5173/"])
        #expect(links.map(\.title) == ["web :5173"])
        #expect(BrowserLinks.ports(session: "s1", state: Seed.window, host: "devbox").map(\.port) == [3000])
        #expect(BrowserLinks.ports(session: "nope", state: Seed.window, host: "devbox").isEmpty)
    }

    @Test(arguments: DevicePreset.allCases)
    func eachDevicePresetHasATitleAndOnlyDesktopFillsTheView(device: DevicePreset) {
        #expect(!device.rawValue.isEmpty)
        #expect((device.size == nil) == (device == .desktop))
    }

    @Test func aNewPageHasOneBlankTabOnTheDesktopAndNoAgent() {
        let page = BrowserPage()
        #expect(page.tabs.count == 1)
        #expect(page.selectedTab?.url == "")
        #expect(page.device == .desktop)
        #expect(!page.agentControl)
    }

    @Test func tabsOpenSelectAndClose() throws {
        let page = BrowserPage()
        let first = try #require(page.selectedTab).id
        page.open("http://devbox:3000/")
        #expect(page.tabs.count == 2)
        #expect(page.selectedTab?.url == "http://devbox:3000/")
        page.select(first)
        #expect(page.selected == first)
        page.close(first)
        #expect(page.tabs.count == 1)
        #expect(page.selectedTab?.url == "http://devbox:3000/")
    }

    @Test func closingTheLastTabLeavesOneBlankTab() throws {
        let page = BrowserPage()
        page.close(try #require(page.selectedTab).id)
        #expect(page.tabs.count == 1)
        #expect(page.selectedTab?.url == "")
    }

    @Test func theAddressBarNavigatesTheSelectedTabAndRefusesBadInput() {
        let page = BrowserPage()
        #expect(page.navigate("3000", host: "devbox"))
        #expect(page.selectedTab?.url == "http://devbox:3000/")
        #expect(page.tabs.count == 1)
        #expect(!page.navigate("javascript:alert(1)", host: "devbox"))
        #expect(page.selectedTab?.url == "http://devbox:3000/")
    }

    @Test func theWebViewReportsTheURLAndTitleOfATab() throws {
        let page = BrowserPage()
        let id = try #require(page.selectedTab).id
        page.update(id, url: "http://devbox:3000/login", title: "Log in")
        #expect(page.selectedTab?.url == "http://devbox:3000/login")
        #expect(page.selectedTab?.title == "Log in")
        #expect(page.selectedTab?.label == "Log in")
        page.update(id, url: "http://devbox:3000/x", title: "")
        #expect(page.selectedTab?.label == "devbox:3000")
    }

    @Test func aLateReportOfTheOldPageDoesNotUndoANewAddress() throws {
        let page = BrowserPage()
        let id = try #require(page.selectedTab).id
        page.update(id, url: "http://devbox:3000/old", title: "Old")
        page.navigate("3001", host: "devbox")
        page.update(id, url: "http://devbox:3000/old", title: "Old, renamed")
        #expect(page.selectedTab?.url == "http://devbox:3001/")
        page.loading(id)
        page.update(id, url: "http://devbox:3001/login", title: "Log in")
        #expect(page.selectedTab?.url == "http://devbox:3001/login")
        #expect(page.selectedTab?.title == "Log in")
    }

    @Test func theUserTakesOverFromTheAgent() {
        let page = BrowserPage()
        page.agentStarted()
        #expect(page.agentControl)
        page.takeOver()
        #expect(!page.agentControl)
    }

    @Test func eachSessionHasItsOwnPage() {
        let browser = Browser()
        browser.page(session: "s1").open("http://a/")
        #expect(browser.page(session: "s1").tabs.count == 2)
        #expect(browser.page(session: "s2").tabs.count == 1)
        #expect(browser.page(session: "s1") === browser.page(session: "s1"))
    }

    @Test func thereIsAlwaysADefaultProfile() {
        let (defaults, suite) = freshDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let profiles = BrowserProfiles(defaults: defaults)
        #expect(profiles.profiles.map(\.name) == ["Default"])
        #expect(profiles.defaultProfile.name == "Default")
        #expect(profiles.profile(for: "s1") == profiles.defaultProfile)
    }

    @Test func profilesAreCreatedClonedAndMadeDefault() throws {
        let (defaults, suite) = freshDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let profiles = BrowserProfiles(defaults: defaults)
        let work = try profiles.create("Work")
        let copy = try profiles.clone(work.id, name: "Work copy")
        #expect(copy.clonedFrom == work.id)
        #expect(copy.id != work.id)
        profiles.setDefault(copy.id)
        #expect(profiles.defaultProfile == copy)
        #expect(profiles.profiles.map(\.name) == ["Default", "Work", "Work copy"])
    }

    @Test func aProfileNeedsAFreeNonEmptyName() throws {
        let (defaults, suite) = freshDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let profiles = BrowserProfiles(defaults: defaults)
        _ = try profiles.create("Work")
        #expect(throws: ProfileRefusal.empty) { try profiles.create("  ") }
        #expect(throws: ProfileRefusal.taken("Work")) { try profiles.create("work") }
        #expect(throws: ProfileRefusal.unknown) { try profiles.clone("nope", name: "X") }
    }

    @Test func aSessionPicksAProfileAndItSurvivesARelaunch() throws {
        let (defaults, suite) = freshDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let profiles = BrowserProfiles(defaults: defaults)
        let work = try profiles.create("Work")
        profiles.assign(session: "s1", profile: work.id)
        profiles.setDefault(work.id)
        let relaunched = BrowserProfiles(defaults: UserDefaults(suiteName: suite)!)
        #expect(relaunched.profiles == profiles.profiles)
        #expect(relaunched.profile(for: "s1") == work)
        #expect(relaunched.defaultProfile == work)
    }

    @Test func theChromeOfAPageNamesItsProfileAndLinks() {
        let page = BrowserPage()
        page.agentStarted()
        let chrome = BrowserChrome(page: page, profile: BrowserProfile(id: "default", name: "Default"), links: BrowserLinks.ports(session: "s1", state: Seed.window, host: "devbox"))
        #expect(chrome.profile == "Default")
        #expect(chrome.links.map(\.port) == [3000])
        #expect(chrome.banner == "An agent controls this tab")
        #expect(chrome.tabs.count == 1)
    }
}
