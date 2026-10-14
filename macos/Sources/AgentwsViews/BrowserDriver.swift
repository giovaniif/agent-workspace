#if canImport(SwiftUI) && canImport(WebKit) && canImport(AppKit)
import AgentwsKit
import AppKit
import Foundation
import WebKit

@MainActor
public final class BrowserDriver {
    private let browser: Browser
    private let profiles: BrowserProfiles
    private let webViews: BrowserWebViews
    private let host: @MainActor () -> String
    private var guards: [String: AgentGuard] = [:]

    public init(browser: Browser, profiles: BrowserProfiles, webViews: BrowserWebViews, host: @escaping @MainActor () -> String) {
        self.browser = browser
        self.profiles = profiles
        self.webViews = webViews
        self.host = host
    }

    public func run(_ call: BrowserCall) async -> BrowserOutcome {
        let step: BrowserStep
        do {
            step = try BrowserStep.of(call)
        } catch {
            return .error(error.message)
        }
        let page = browser.page(session: call.session)
        page.agentStarted()
        let policy = BrowserPolicy(serverHost: host())
        let tab = page.selected
        let profile = profiles.profile(for: call.session).id
        let view = webViews.webView(tab: tab, profile: profile)
        if view.frame.isEmpty { view.frame = CGRect(x: 0, y: 0, width: 1280, height: 800) }
        let watcher = guards[tab] ?? AgentGuard(policy: policy, active: { [weak page] in page?.agentControl ?? false })
        watcher.policy = policy
        guards[tab] = watcher
        view.navigationDelegate = watcher
        switch step {
        case let .goto(input):
            guard let url = BrowserAddress.url(from: input, host: policy.serverHost), let target = URL(string: url) else {
                return .error("not an address: \(input)")
            }
            if let reason = await Self.check(url, policy) { return .error(reason) }
            page.navigate(url, host: policy.serverHost)
            webViews.requested[tab] = url
            watcher.refused = nil
            view.load(URLRequest(url: target))
            page.loading(tab)
            await settle(view)
            if let reason = watcher.refused { return .error(reason) }
            let now = view.url?.absoluteString ?? url
            return .result(.object(["text": .string("opened \(now)"), "url": .string(now), "title": .string(view.title ?? "")]))
        case let .script(body, args):
            if let reason = await recheck(view, policy) { return .error(reason) }
            do {
                let value = try await view.callAsyncJavaScript(body, arguments: args, in: nil, contentWorld: .page)
                await settle(view)
                if let reason = watcher.refused {
                    watcher.refused = nil
                    return .error(reason)
                }
                return .result(Self.json(value))
            } catch {
                return .error(Self.message(error))
            }
        case let .until(body, args):
            let deadline = ContinuousClock.now + .seconds(10)
            while ContinuousClock.now < deadline {
                if let found = try? await view.callAsyncJavaScript(body, arguments: args, in: nil, contentWorld: .page) as? Bool, found {
                    return .result(.string("found"))
                }
                try? await Task.sleep(for: .milliseconds(100))
            }
            return .error("timed out after 10 s")
        case let .sleep(ms):
            try? await Task.sleep(for: .milliseconds(ms))
            return .result(.string("waited \(ms) ms"))
        case .screenshot:
            if let reason = await recheck(view, policy) { return .error(reason) }
            do {
                let image = try await view.takeSnapshot(configuration: nil)
                guard let tiff = image.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff),
                      let png = bitmap.representation(using: .png, properties: [:]) else { return .error("could not make a PNG") }
                return .result(.object(["png": .string(png.base64EncodedString()), "width": .number(Double(bitmap.pixelsWide)), "height": .number(Double(bitmap.pixelsHigh))]))
            } catch {
                return .error(Self.message(error))
            }
        case .tabs:
            let rows = page.tabs.map { tab in
                JSONValue.object(["id": .string(tab.id), "url": .string(tab.url), "title": .string(tab.title), "selected": .bool(tab.id == page.selected)])
            }
            let text = page.tabs.map { ($0.id == page.selected ? "* " : "  ") + ($0.url.isEmpty ? "(empty)" : $0.url) + ($0.title.isEmpty ? "" : "  " + $0.title) }
            return .result(.object(["text": .string(text.joined(separator: "\n")), "tabs": .array(rows)]))
        case let .cookies(domain):
            let all = await webViews.store(for: profile).httpCookieStore.allCookies()
            let picked = all.filter { cookie in
                guard let domain else { return true }
                let host = cookie.domain.hasPrefix(".") ? String(cookie.domain.dropFirst()) : cookie.domain
                return host == domain || host.hasSuffix("." + domain) || domain.hasSuffix("." + host)
            }
            let rows = picked.map { JSONValue.object(["name": .string($0.name), "domain": .string($0.domain), "path": .string($0.path)]) }
            let text = picked.map { "\($0.domain)\t\($0.name)" }.joined(separator: "\n")
            return .result(.object(["text": .string(text.isEmpty ? "no cookies" : text), "cookies": .array(rows)]))
        case let .device(device):
            page.device = device
            if view.customUserAgent != device.userAgent {
                view.customUserAgent = device.userAgent
                if view.url != nil { view.reload() }
            }
            if let size = device.size { view.frame.size = CGSize(width: size.width, height: size.height) }
            return .result(.string("device \(device.rawValue)"))
        }
    }

    private func settle(_ view: WKWebView) async {
        try? await Task.sleep(for: .milliseconds(50))
        let deadline = ContinuousClock.now + .seconds(30)
        while view.isLoading, ContinuousClock.now < deadline {
            try? await Task.sleep(for: .milliseconds(100))
        }
    }

    private func recheck(_ view: WKWebView, _ policy: BrowserPolicy) async -> String? {
        guard let url = view.url?.absoluteString else { return nil }
        if let reason = await Self.check(url, policy) { return reason }
        let frames = (try? await view.callAsyncJavaScript(BrowserScript.frames, arguments: [:], in: nil, contentWorld: .defaultClient)) as? [String] ?? []
        for frame in frames {
            if let reason = await Self.check(frame, policy) { return reason }
        }
        return nil
    }

    static func check(_ url: String, _ policy: BrowserPolicy) async -> String? {
        guard let host = policy.needsLookup(url: url) else { return policy.refusal(url: url, addresses: []) }
        let addresses = await HostLookup.addresses(host)
        return policy.refusal(url: url, addresses: addresses)
    }

    static func message(_ error: Error) -> String {
        let ns = error as NSError
        if let text = ns.userInfo["WKJavaScriptExceptionMessage"] as? String { return text }
        return ns.localizedDescription
    }

    static func json(_ value: Any?) -> JSONValue {
        switch value {
        case nil, is NSNull:
            return .null
        case let number as NSNumber:
            if CFGetTypeID(number) == CFBooleanGetTypeID() { return .bool(number.boolValue) }
            return .number(number.doubleValue)
        case let text as String:
            return .string(text)
        case let list as [Any]:
            return .array(list.map { json($0) })
        case let map as [String: Any]:
            return .object(map.mapValues { json($0) })
        default:
            return .string(String(describing: value!))
        }
    }
}

@MainActor
final class AgentGuard: NSObject, WKNavigationDelegate {
    var policy: BrowserPolicy
    var refused: String?
    private let active: @MainActor () -> Bool

    init(policy: BrowserPolicy, active: @escaping @MainActor () -> Bool) {
        self.policy = policy
        self.active = active
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction) async -> WKNavigationActionPolicy {
        guard active(), let url = navigationAction.request.url?.absoluteString else { return .allow }
        if let reason = await BrowserDriver.check(url, policy) {
            refused = "refused \(url): \(reason)"
            return .cancel
        }
        return .allow
    }
}

enum HostLookup {
    static func addresses(_ host: String) async -> [String] {
        await Task.detached { resolve(host) }.value
    }

    nonisolated static func resolve(_ host: String) -> [String] {
        var hints = addrinfo()
        hints.ai_socktype = SOCK_STREAM
        var list: UnsafeMutablePointer<addrinfo>?
        guard getaddrinfo(host, nil, &hints, &list) == 0, let first = list else { return [] }
        defer { freeaddrinfo(first) }
        var out: [String] = []
        var cursor: UnsafeMutablePointer<addrinfo>? = first
        while let info = cursor {
            var buffer = [CChar](repeating: 0, count: Int(NI_MAXHOST))
            if getnameinfo(info.pointee.ai_addr, info.pointee.ai_addrlen, &buffer, socklen_t(buffer.count), nil, 0, NI_NUMERICHOST) == 0 {
                let bytes = buffer.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }
                out.append(String(decoding: bytes, as: UTF8.self))
            }
            cursor = info.pointee.ai_next
        }
        return out
    }
}
#endif
