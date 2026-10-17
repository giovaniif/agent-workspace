#if canImport(SwiftUI) && canImport(WebKit) && canImport(Security)
import CrewKit
import AppKit
import Security
import SwiftUI
import UniformTypeIdentifiers
import WebKit

enum CookieReader {
    static func password(service: String) throws -> String {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        guard status == errSecSuccess, let data = item as? Data else {
            throw CookieImportError("no \(service) key in the Keychain, or access was denied")
        }
        return String(decoding: data, as: UTF8.self)
    }

    static func read(_ profile: CookieProfile) throws -> [CookieRecord] {
        guard let service = profile.browser.keychainService else {
            do {
                return try BinaryCookies.parse(Data(contentsOf: profile.file))
            } catch let error as CookieImportError {
                throw error
            } catch {
                throw CookieImportError("Safari cookies need Full Disk Access for crew (System Settings › Privacy & Security)")
            }
        }
        return try ChromeCookies.read(file: profile.file, password: password(service: service))
    }

    static func cookie(_ record: CookieRecord) -> HTTPCookie? {
        var properties: [HTTPCookiePropertyKey: Any] = [
            .name: record.name, .value: record.value, .domain: record.domain, .path: record.path,
        ]
        if let expires = record.expires { properties[.expires] = expires }
        if record.secure { properties[.secure] = "TRUE" }
        if record.httpOnly { properties[HTTPCookiePropertyKey("HttpOnly")] = "TRUE" }
        return HTTPCookie(properties: properties)
    }

    static func record(_ cookie: HTTPCookie) -> CookieRecord {
        CookieRecord(
            name: cookie.name, value: cookie.value, domain: cookie.domain, path: cookie.path,
            expires: cookie.expiresDate, secure: cookie.isSecure, httpOnly: cookie.isHTTPOnly
        )
    }

    @MainActor
    static func store(_ records: [CookieRecord], domains: Set<String>, into store: WKHTTPCookieStore) async {
        for old in await store.allCookies() where domains.contains(CookieImport.site(of: old.domain)) {
            await store.deleteCookie(old)
        }
        for record in records {
            if let cookie = cookie(record) { await store.setCookie(cookie) }
        }
    }
}

enum CookieSheetTarget {
    case profile
    case server(ServerProfileSend)
}

struct CookieImportSheet: View {
    let profile: BrowserProfile
    let store: WKHTTPCookieStore
    var target: CookieSheetTarget = .profile
    let done: () -> Void
    @State private var sources: [CookieProfile] = []
    @State private var records: [CookieRecord] = []
    @State private var chosen: Set<String> = []
    @State private var message: String?
    @State private var imported: [DomainCount] = []

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title).font(.headline)
            Text(subtitle).font(.system(size: 12)).foregroundStyle(.secondary)
            HStack {
                if case .server = target {
                    Button("From \(profile.name)") { loadProfile() }
                }
                Menu("From a browser") {
                    ForEach(sources) { source in
                        Button(source.label) { load { try CookieReader.read(source) } }
                    }
                    if sources.isEmpty { Text("No Chrome, Arc, Brave, Edge or Safari profile found") }
                }
                .fixedSize()
                Button("From a file…") { openFile() }
            }
            if !records.isEmpty {
                List(CookieImport.summary(records)) { row in
                    Toggle(isOn: Binding(get: { chosen.contains(row.domain) }, set: { on in
                        if on { chosen.insert(row.domain) } else { chosen.remove(row.domain) }
                    })) {
                        HStack {
                            Text(row.domain)
                            Spacer()
                            Text("\(row.count)").foregroundStyle(.secondary)
                        }
                    }
                }
                .frame(minHeight: 180)
            }
            if case let .server(send) = target, let status = send.status {
                Text(status).font(.system(size: 12))
            }
            if !imported.isEmpty {
                Text("Imported: " + imported.map { "\($0.domain) \($0.count)" }.joined(separator: ", ")).font(.system(size: 12))
            }
            if let message {
                Text(message).font(.system(size: 12)).foregroundStyle(.red)
            }
            HStack {
                Spacer()
                Button("Close", action: done).keyboardShortcut(.cancelAction)
                switch target {
                case .profile:
                    Button("Import") { importChosen() }
                        .keyboardShortcut(.defaultAction)
                        .disabled(chosen.isEmpty)
                case let .server(send):
                    Button("Send to server") { sendChosen(send) }
                        .keyboardShortcut(.defaultAction)
                        .disabled(chosen.isEmpty || send.sending)
                }
            }
        }
        .padding(20)
        .frame(width: 460)
        .onAppear { sources = CookieSources.profiles(home: FileManager.default.homeDirectoryForCurrentUser) }
    }

    private var title: String {
        switch target {
        case .profile: "Import cookies into \(profile.name)"
        case .server: "Send cookies to the server"
        }
    }

    private var subtitle: String {
        switch target {
        case .profile: "Cookies stay on this Mac. They never go to the server."
        case .server: "Only the sites that you select go to the headless browser of the server. Other cookies stay on this Mac."
        }
    }

    private func loadProfile() {
        Task {
            let cookies = await store.allCookies()
            load { cookies.map(CookieReader.record) }
        }
    }

    private func sendChosen(_ send: ServerProfileSend) {
        let picked = records
        let domains = chosen
        Task { await send.send(picked, domains: domains) }
    }

    private func load(_ read: () throws -> [CookieRecord]) {
        do {
            records = try read()
            chosen = []
            imported = []
            message = records.isEmpty ? "That source has no cookies." : nil
        } catch let error as CookieImportError {
            message = error.message
        } catch {
            message = error.localizedDescription
        }
    }

    private func openFile() {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.json, .plainText, .data]
        panel.allowsMultipleSelection = false
        guard panel.runModal() == .OK, let url = panel.url else { return }
        load {
            let data = try Data(contentsOf: url)
            return url.pathExtension == "binarycookies" ? try BinaryCookies.parse(data) : try CookieFile.parse(data)
        }
    }

    private func importChosen() {
        let picked = CookieImport.only(records, domains: chosen)
        let domains = chosen
        Task {
            await CookieReader.store(picked, domains: domains, into: store)
            imported = CookieImport.summary(picked)
        }
    }
}
#endif
