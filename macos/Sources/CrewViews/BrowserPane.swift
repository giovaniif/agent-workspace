#if canImport(SwiftUI)
import CrewKit
import SwiftUI
#if canImport(AppKit)
import AppKit
#endif
#if canImport(WebKit)
import WebKit
#endif

public enum BrowserAction: Equatable {
    case navigate(String)
    case open(String)
    case select(String)
    case close(String)
    case back
    case forward
    case reload
    case device(DevicePreset)
    case profile(String)
    case newProfile
    case cloneProfile
    case makeDefault
    case importCookies
    case sendToServer
    case takeOver
}

struct BrowserPane: View {
    let chrome: BrowserChrome
    let act: (BrowserAction) -> Void
    @Environment(\.colorScheme) private var scheme
    @State private var address = ""
    @State private var editing = false
    @State private var serverTab = false
#if canImport(WebKit)
    @Environment(\.crewBrowser) private var webViews
#endif

    var body: some View {
        let theme = Theme(scheme)
        VStack(spacing: 0) {
            tabStrip(theme)
            Rectangle().fill(theme(.surface)).frame(height: 1)
            addressBar(theme)
            if !chrome.links.isEmpty {
                linkRow(theme)
            }
            Rectangle().fill(theme(.surface)).frame(height: 1)
            content(theme)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(chrome.device == .desktop ? theme(.base) : theme(.crust))
                .overlay(alignment: .top) {
                    if let banner = chrome.banner {
                        agentBanner(banner, theme)
                    }
                }
        }
        .onAppear { address = chrome.address }
        .onChange(of: chrome.address) { if !editing { address = chrome.address } }
        .onChange(of: chrome.selected) { address = chrome.address }
    }

    private func tabStrip(_ theme: Theme) -> some View {
        HStack(spacing: 4) {
            ForEach(chrome.tabs) { tab in
                let active = tab.id == chrome.selected
                HStack(spacing: 6) {
                    Text(tab.label).font(.system(size: 12, weight: active ? .semibold : .regular)).lineLimit(1)
                    Button { act(.close(tab.id)) } label: {
                        Image(systemName: "xmark").font(.system(size: 9, weight: .bold)).foregroundStyle(theme(.subtext))
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Close tab")
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
                .frame(maxWidth: 200)
                .background(RoundedRectangle(cornerRadius: 5).fill(active ? theme(.base) : .clear))
                .contentShape(Rectangle())
                .onTapGesture {
                    serverTab = false
                    act(.select(tab.id))
                }
            }
            if chrome.headless != nil {
                HStack(spacing: 6) {
                    Image(systemName: "server.rack").font(.system(size: 10))
                    Text("Server · read-only").font(.system(size: 12, weight: showsServer ? .semibold : .regular)).lineLimit(1)
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
                .background(RoundedRectangle(cornerRadius: 5).fill(showsServer ? theme(.base) : .clear))
                .contentShape(Rectangle())
                .onTapGesture { serverTab = true }
                .accessibilityLabel("Headless tab on the server")
            }
            Button { act(.open("")) } label: {
                Image(systemName: "plus").font(.system(size: 11, weight: .semibold)).foregroundStyle(theme(.subtext))
            }
            .buttonStyle(.plain)
            .accessibilityLabel("New tab")
            Spacer()
        }
        .padding(.horizontal, Metrics.gutter)
        .frame(height: 32)
        .background(theme(.crust))
    }

    private func addressBar(_ theme: Theme) -> some View {
        HStack(spacing: 10) {
            Button { act(.back) } label: { Image(systemName: "chevron.left") }
                .buttonStyle(.plain).accessibilityLabel("Back")
            Button { act(.forward) } label: { Image(systemName: "chevron.right") }
                .buttonStyle(.plain).accessibilityLabel("Forward")
            Button { act(.reload) } label: { Image(systemName: "arrow.clockwise") }
                .buttonStyle(.plain).accessibilityLabel("Reload")
            TextField("Address, port or URL", text: $address, onEditingChanged: { editing = $0 })
                .textFieldStyle(.plain)
                .font(Metrics.mono)
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(RoundedRectangle(cornerRadius: 5).fill(theme(.base)))
                .onSubmit { act(.navigate(address)) }
            Picker("Device", selection: Binding(get: { chrome.device }, set: { act(.device($0)) })) {
                ForEach(DevicePreset.allCases, id: \.self) { device in
                    Text(device.rawValue).tag(device)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .fixedSize()
            Menu {
                ForEach(chrome.profiles) { profile in
                    Button(profile.name == chrome.profile ? "✓ " + profile.name : profile.name) { act(.profile(profile.id)) }
                }
                Divider()
                Button("New profile…") { act(.newProfile) }
                Button("Clone this profile…") { act(.cloneProfile) }
                Button("Make this profile the default") { act(.makeDefault) }
                Button("Import cookies…") { act(.importCookies) }
                Button("Send cookies to the server…") { act(.sendToServer) }
            } label: {
                Label(chrome.profile, systemImage: "person.crop.circle").font(.system(size: 12))
            }
            .menuStyle(.borderlessButton)
            .fixedSize()
        }
        .font(.system(size: 12))
        .foregroundStyle(theme(.text))
        .padding(.horizontal, Metrics.gutter)
        .frame(height: 36)
        .background(theme(.mantle))
    }

    private func linkRow(_ theme: Theme) -> some View {
        HStack(spacing: 6) {
            Text("Ports").font(.system(size: 11, weight: .medium)).foregroundStyle(theme(.subtext))
            ForEach(chrome.links) { link in
                Button { act(.navigate(link.url)) } label: {
                    Text(link.title)
                        .font(Metrics.mono)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 2)
                        .background(Capsule().stroke(theme(.surface)))
                }
                .buttonStyle(.plain)
                .help(link.url)
            }
            Spacer()
        }
        .padding(.horizontal, Metrics.gutter)
        .frame(height: 26)
        .background(theme(.mantle))
    }

    private func agentBanner(_ text: String, _ theme: Theme) -> some View {
        HStack(spacing: 10) {
            Image(systemName: "sparkles").foregroundStyle(theme(.peach))
            Text(text).font(.system(size: 12, weight: .semibold))
            Spacer()
            Button("Take over") { act(.takeOver) }
                .buttonStyle(.plain)
                .font(.system(size: 12, weight: .semibold))
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
                .background(RoundedRectangle(cornerRadius: 5).fill(theme(.peach)))
                .foregroundStyle(theme(.base))
        }
        .padding(.horizontal, Metrics.gutter)
        .frame(height: 34)
        .background(theme(.peach).opacity(0.18))
    }

    @ViewBuilder
    private func content(_ theme: Theme) -> some View {
        GeometryReader { geometry in
            let frame = fitted(chrome.device, in: geometry.size)
            page(theme)
                .frame(width: frame.width, height: frame.height)
                .clipShape(RoundedRectangle(cornerRadius: chrome.device == .desktop ? 0 : 12))
                .overlay {
                    if chrome.device != .desktop {
                        RoundedRectangle(cornerRadius: 12).stroke(theme(.surface))
                    }
                }
                .frame(width: geometry.size.width, height: geometry.size.height)
        }
    }

    private func fitted(_ device: DevicePreset, in space: CGSize) -> CGSize {
        guard let size = device.size else { return space }
        let available = CGSize(width: max(space.width - 32, 1), height: max(space.height - 32, 1))
        let scale = min(1, available.width / CGFloat(size.width), available.height / CGFloat(size.height))
        return CGSize(width: CGFloat(size.width) * scale, height: CGFloat(size.height) * scale)
    }

    private var showsServer: Bool {
        serverTab && chrome.headless != nil
    }

    @ViewBuilder
    private func page(_ theme: Theme) -> some View {
#if canImport(AppKit)
        if showsServer, let frame = chrome.headless, let image = NSImage(data: frame.png) {
            Image(nsImage: image)
                .resizable()
                .aspectRatio(contentMode: .fit)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(theme(.crust))
                .accessibilityLabel("Screenshot of the headless tab")
        } else {
            localPage(theme)
        }
#else
        localPage(theme)
#endif
    }

    @ViewBuilder
    private func localPage(_ theme: Theme) -> some View {
#if canImport(WebKit)
        if let webViews, let tab = chrome.tabs.first(where: { $0.id == chrome.selected }), !tab.url.isEmpty {
            WebTab(views: webViews, tab: tab, device: chrome.device)
                .id(tab.id)
        } else {
            placeholder(theme)
        }
#else
        placeholder(theme)
#endif
    }

    private func placeholder(_ theme: Theme) -> some View {
        VStack(spacing: 8) {
            Image(systemName: "globe").font(.system(size: 28)).foregroundStyle(theme(.grey))
            Text(chrome.address.isEmpty ? "Type an address, a port or a URL" : chrome.address)
                .font(.system(size: 13))
                .foregroundStyle(theme(.subtext))
            if chrome.address.isEmpty, let link = chrome.links.first {
                Text("or open \(link.title)").font(.system(size: 12)).foregroundStyle(theme(.subtext))
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(theme(.base))
    }
}

#if canImport(WebKit)
@MainActor
public final class BrowserWebViews {
    private var views: [String: WKWebView] = [:]
    private var observers: [String: [NSKeyValueObservation]] = [:]
    private var stores: [String: WKWebsiteDataStore] = [:]
    var requested: [String: String] = [:]
    private var reported: [String: [String]] = [:]
    public var report: @MainActor (String, String, String) -> Void = { _, _, _ in }
    public var loading: @MainActor (String) -> Void = { _ in }
    public var profile: @MainActor (String) -> String = { _ in BrowserProfile.standard.id }

    public init() {}

    public func store(for profile: String) -> WKWebsiteDataStore {
        if let store = stores[profile] { return store }
        let store: WKWebsiteDataStore
        if profile == BrowserProfile.standard.id {
            store = .default()
        } else if let uuid = UUID(uuidString: profile) {
            store = WKWebsiteDataStore(forIdentifier: uuid)
        } else {
            store = .nonPersistent()
        }
        stores[profile] = store
        return store
    }

    public func webView(for tab: String) -> WKWebView? {
        views[tab]
    }

    func webView(tab: String, profile: String) -> WKWebView {
        if let view = views[tab] { return view }
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = store(for: profile)
        let view = WKWebView(frame: .zero, configuration: configuration)
        view.allowsBackForwardNavigationGestures = true
        let publish: @MainActor (WKWebView) -> Void = { [weak self] web in
            let url = web.url?.absoluteString ?? ""
            let title = web.title ?? ""
            guard let self, self.reported[tab] != [url, title] else { return }
            self.reported[tab] = [url, title]
            if !url.isEmpty { self.requested[tab] = url }
            self.report(tab, url, title)
        }
        observers[tab] = [
            view.observe(\.url, options: [.new]) { web, _ in MainActor.assumeIsolated { publish(web) } },
            view.observe(\.title, options: [.new]) { web, _ in MainActor.assumeIsolated { publish(web) } },
        ]
        views[tab] = view
        return view
    }

    public func close(_ tab: String) {
        observers[tab] = nil
        requested[tab] = nil
        reported[tab] = nil
        views.removeValue(forKey: tab)?.removeFromSuperview()
    }

    public func clone(from source: String, to target: String) async {
        let cookies = await store(for: source).httpCookieStore.allCookies()
        let destination = store(for: target).httpCookieStore
        for cookie in cookies {
            await destination.setCookie(cookie)
        }
    }
}

struct WebTab: NSViewRepresentable {
    let views: BrowserWebViews
    let tab: BrowserTab
    let device: DevicePreset

    func makeNSView(context: Context) -> WKWebView {
        let view = views.webView(tab: tab.id, profile: views.profile(tab.id))
        sync(view)
        return view
    }

    func updateNSView(_ view: WKWebView, context: Context) {
        sync(view)
    }

    private func sync(_ view: WKWebView) {
        if view.customUserAgent != device.userAgent {
            view.customUserAgent = device.userAgent
            if view.url != nil { view.reload() }
        }
        defer { views.loading(tab.id) }
        guard !tab.url.isEmpty, views.requested[tab.id] != tab.url, view.url?.absoluteString != tab.url, let url = URL(string: tab.url) else { return }
        views.requested[tab.id] = tab.url
        view.load(URLRequest(url: url))
    }
}

private struct BrowserKey: EnvironmentKey {
    static let defaultValue: BrowserWebViews? = nil
}

extension EnvironmentValues {
    public var crewBrowser: BrowserWebViews? {
        get { self[BrowserKey.self] }
        set { self[BrowserKey.self] = newValue }
    }
}
#endif
#endif
