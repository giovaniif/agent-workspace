import Foundation
import Observation

public enum DevicePreset: String, Sendable, Equatable, CaseIterable, Codable {
    case desktop = "Desktop"
    case ipad = "iPad"
    case iphone = "iPhone"

    public var size: (width: Int, height: Int)? {
        switch self {
        case .desktop: nil
        case .ipad: (820, 1180)
        case .iphone: (393, 852)
        }
    }

    public var userAgent: String? {
        switch self {
        case .desktop: nil
        case .ipad: "Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"
        case .iphone: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"
        }
    }
}

public enum BrowserAddress {
    static let loopback: Set<String> = ["localhost", "127.0.0.1", "0.0.0.0"]

    public static func url(from input: String, host: String) -> String? {
        let text = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !text.contains(where: \.isWhitespace) else { return nil }
        let bare = text.hasPrefix(":") ? String(text.dropFirst()) : text
        if let port = Int(bare), (1...65535).contains(port) {
            return "http://\(host):\(port)/"
        }
        if text.contains("://") {
            guard let parts = URLComponents(string: text), let scheme = parts.scheme?.lowercased(),
                  scheme == "http" || scheme == "https", !(parts.host ?? "").isEmpty else { return nil }
            return text
        }
        let slash = text.firstIndex(of: "/") ?? text.endIndex
        let authority = String(text[..<slash])
        let path = slash == text.endIndex ? "/" : String(text[slash...])
        let name = authority.split(separator: ":", maxSplits: 1).first.map(String.init) ?? ""
        if loopback.contains(name.lowercased()) {
            let port = authority.dropFirst(name.count)
            guard port.isEmpty || Int(port.dropFirst()).map({ (1...65535).contains($0) }) == true else { return nil }
            return "http://\(host)\(port)\(path)"
        }
        guard name.contains("."), !name.hasPrefix("."), !name.hasSuffix("."),
              URLComponents(string: "https://" + text)?.host != nil else { return nil }
        return "https://" + text
    }
}

public enum BrowserHost {
    public static func of(_ server: ServerKind) -> String {
        switch server {
        case .thisMac: "localhost"
        case let .ssh(host): String(host.split(separator: "@").last ?? Substring(host))
        }
    }
}

public struct PortLink: Sendable, Equatable, Identifiable {
    public var id: Int { port }
    public var port: Int
    public var title: String
    public var url: String
}

public enum BrowserLinks {
    public static func ports(session: String, state: ViewState, host: String) -> [PortLink] {
        var seen = Set<Int>()
        var links: [PortLink] = []
        for worktree in state.worktrees where worktree.sessionID == session {
            for port in worktree.ports ?? [] where seen.insert(port.port).inserted {
                links.append(PortLink(port: port.port, title: "\(worktree.repo) :\(port.port)", url: "http://\(host):\(port.port)/"))
            }
        }
        return links.sorted { $0.port < $1.port }
    }
}

public struct BrowserTab: Sendable, Equatable, Identifiable {
    public var id: String
    public var url: String
    public var title: String

    public init(id: String = UUID().uuidString, url: String = "", title: String = "") {
        self.id = id
        self.url = url
        self.title = title
    }

    public var label: String {
        if !title.isEmpty { return title }
        guard let parts = URLComponents(string: url), let host = parts.host, !host.isEmpty else { return "New tab" }
        return parts.port.map { "\(host):\($0)" } ?? host
    }
}

@MainActor
@Observable
public final class BrowserPage {
    public private(set) var tabs: [BrowserTab]
    public private(set) var selected: String
    public var device: DevicePreset = .desktop
    public private(set) var agentControl = false
    @ObservationIgnored private var pending: Set<String> = []

    public init() {
        let tab = BrowserTab()
        tabs = [tab]
        selected = tab.id
    }

    public var selectedTab: BrowserTab? { tabs.first { $0.id == selected } }

    public func open(_ url: String) {
        let tab = BrowserTab(url: url)
        tabs.append(tab)
        selected = tab.id
    }

    public func select(_ id: String) {
        guard tabs.contains(where: { $0.id == id }) else { return }
        selected = id
    }

    public func close(_ id: String) {
        guard let index = tabs.firstIndex(where: { $0.id == id }) else { return }
        tabs.remove(at: index)
        pending.remove(id)
        if tabs.isEmpty { tabs = [BrowserTab()] }
        if selected == id { selected = tabs[min(index, tabs.count - 1)].id }
    }

    @discardableResult
    public func navigate(_ input: String, host: String) -> Bool {
        guard let url = BrowserAddress.url(from: input, host: host), let index = tabs.firstIndex(where: { $0.id == selected }) else { return false }
        tabs[index].url = url
        tabs[index].title = ""
        pending.insert(selected)
        return true
    }

    public func loading(_ id: String) {
        pending.remove(id)
    }

    public func update(_ id: String, url: String, title: String) {
        guard !pending.contains(id), let index = tabs.firstIndex(where: { $0.id == id }) else { return }
        tabs[index].url = url
        tabs[index].title = title
    }

    public func agentStarted() {
        agentControl = true
    }

    public func takeOver() {
        agentControl = false
    }
}

@MainActor
@Observable
public final class Browser {
    public private(set) var pages: [String: BrowserPage] = [:]

    public init() {}

    public func page(session: String) -> BrowserPage {
        if let page = pages[session] { return page }
        let page = BrowserPage()
        pages[session] = page
        return page
    }
}

public struct BrowserProfile: Codable, Sendable, Equatable, Identifiable {
    public var id: String
    public var name: String
    public var clonedFrom: String?

    public init(id: String, name: String, clonedFrom: String? = nil) {
        self.id = id
        self.name = name
        self.clonedFrom = clonedFrom
    }

    public static let standard = BrowserProfile(id: "default", name: "Default")
}

public enum ProfileRefusal: Error, Equatable, Sendable {
    case empty
    case taken(String)
    case unknown
}

@MainActor
@Observable
public final class BrowserProfiles {
    public static let profilesKey = "browser.profiles"
    public static let defaultKey = "browser.default"
    public static let sessionsKey = "browser.sessions"

    public private(set) var profiles: [BrowserProfile]
    public private(set) var defaultID: String
    public private(set) var sessions: [String: String]
    @ObservationIgnored private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        let decoder = JSONDecoder()
        let stored = defaults.data(forKey: Self.profilesKey).flatMap { try? decoder.decode([BrowserProfile].self, from: $0) } ?? []
        profiles = stored.contains(.standard) ? stored : [.standard] + stored
        defaultID = defaults.string(forKey: Self.defaultKey) ?? BrowserProfile.standard.id
        sessions = defaults.data(forKey: Self.sessionsKey).flatMap { try? decoder.decode([String: String].self, from: $0) } ?? [:]
    }

    public var defaultProfile: BrowserProfile {
        profiles.first { $0.id == defaultID } ?? .standard
    }

    public func profile(for session: String) -> BrowserProfile {
        sessions[session].flatMap { id in profiles.first { $0.id == id } } ?? defaultProfile
    }

    @discardableResult
    public func create(_ name: String) throws -> BrowserProfile {
        try add(name: name, clonedFrom: nil)
    }

    @discardableResult
    public func clone(_ id: String, name: String) throws -> BrowserProfile {
        guard profiles.contains(where: { $0.id == id }) else { throw ProfileRefusal.unknown }
        return try add(name: name, clonedFrom: id)
    }

    public func setDefault(_ id: String) {
        guard profiles.contains(where: { $0.id == id }) else { return }
        defaultID = id
        save()
    }

    public func assign(session: String, profile id: String) {
        guard profiles.contains(where: { $0.id == id }) else { return }
        sessions[session] = id
        save()
    }

    private func add(name: String, clonedFrom: String?) throws -> BrowserProfile {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { throw ProfileRefusal.empty }
        if let taken = profiles.first(where: { $0.name.caseInsensitiveCompare(trimmed) == .orderedSame }) {
            throw ProfileRefusal.taken(taken.name)
        }
        let profile = BrowserProfile(id: UUID().uuidString, name: trimmed, clonedFrom: clonedFrom)
        profiles.append(profile)
        save()
        return profile
    }

    private func save() {
        let encoder = JSONEncoder()
        if let data = try? encoder.encode(profiles) { defaults.set(data, forKey: Self.profilesKey) }
        defaults.set(defaultID, forKey: Self.defaultKey)
        if let data = try? encoder.encode(sessions) { defaults.set(data, forKey: Self.sessionsKey) }
    }
}

public struct BrowserChrome: Sendable, Equatable {
    public var tabs: [BrowserTab]
    public var selected: String
    public var address: String
    public var device: DevicePreset
    public var profile: String
    public var profiles: [BrowserProfile]
    public var links: [PortLink]
    public var banner: String?
    public var headless: HeadlessFrame?

    public static let agentBanner = "An agent controls this tab"

    public init(tabs: [BrowserTab], selected: String, device: DevicePreset, profile: String, profiles: [BrowserProfile] = [], links: [PortLink], agentControl: Bool) {
        self.tabs = tabs
        self.selected = selected
        self.address = tabs.first { $0.id == selected }?.url ?? ""
        self.device = device
        self.profile = profile
        self.profiles = profiles
        self.links = links
        self.banner = agentControl ? Self.agentBanner : nil
    }

    @MainActor
    public init(page: BrowserPage, profile: BrowserProfile, profiles: [BrowserProfile] = [], links: [PortLink]) {
        self.init(tabs: page.tabs, selected: page.selected, device: page.device, profile: profile.name, profiles: profiles, links: links, agentControl: page.agentControl)
    }
}
