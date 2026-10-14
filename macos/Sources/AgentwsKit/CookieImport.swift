import Foundation

public struct CookieRecord: Sendable, Equatable {
    public var name: String
    public var value: String
    public var domain: String
    public var path: String
    public var expires: Date?
    public var secure: Bool
    public var httpOnly: Bool

    public init(name: String, value: String, domain: String, path: String, expires: Date?, secure: Bool, httpOnly: Bool) {
        self.name = name
        self.value = value
        self.domain = domain
        self.path = path
        self.expires = expires
        self.secure = secure
        self.httpOnly = httpOnly
    }
}

public struct CookieImportError: Error, Equatable, Sendable {
    public var message: String
    public init(_ message: String) { self.message = message }
}

public struct DomainCount: Sendable, Equatable, Identifiable {
    public var id: String { domain }
    public var domain: String
    public var count: Int
}

public enum CookieFile {
    public static func parse(_ data: Data) throws -> [CookieRecord] {
        if let json = try? JSONDecoder().decode(JSONValue.self, from: data) {
            switch json {
            case let .array(items):
                return items.compactMap(record)
            case let .object(object):
                guard case let .array(items)? = object["cookies"] else { throw CookieImportError("the JSON has no cookies list") }
                return items.compactMap(record)
            default:
                throw CookieImportError("the JSON is not a cookie list")
            }
        }
        let cookies = netscape(String(decoding: data, as: UTF8.self))
        guard !cookies.isEmpty else { throw CookieImportError("no cookies: use a Netscape cookies.txt, a JSON cookie list or a Playwright storageState") }
        return cookies
    }

    static func netscape(_ text: String) -> [CookieRecord] {
        var out: [CookieRecord] = []
        for raw in text.split(whereSeparator: \.isNewline) {
            var line = String(raw)
            var httpOnly = false
            if line.hasPrefix("#HttpOnly_") {
                httpOnly = true
                line.removeFirst("#HttpOnly_".count)
            } else if line.hasPrefix("#") {
                continue
            }
            let f = line.split(separator: "\t", omittingEmptySubsequences: false).map(String.init)
            guard f.count == 7, let seconds = Double(f[4]) else { continue }
            out.append(CookieRecord(
                name: f[5], value: f[6], domain: f[0], path: f[2],
                expires: seconds > 0 ? Date(timeIntervalSince1970: seconds) : nil,
                secure: f[3].uppercased() == "TRUE", httpOnly: httpOnly
            ))
        }
        return out
    }

    static func record(_ value: JSONValue) -> CookieRecord? {
        guard case let .object(o) = value, let name = o["name"]?.string, let domain = o["domain"]?.string else { return nil }
        let seconds = o["expires"]?.number ?? o["expirationDate"]?.number ?? -1
        return CookieRecord(
            name: name, value: o["value"]?.string ?? "", domain: domain, path: o["path"]?.string ?? "/",
            expires: seconds > 0 && o["session"]?.bool != true ? Date(timeIntervalSince1970: seconds) : nil,
            secure: o["secure"]?.bool ?? false, httpOnly: o["httpOnly"]?.bool ?? false
        )
    }
}

public enum BinaryCookies {
    static let macEpoch = 978_307_200.0

    public static func parse(_ data: Data) throws -> [CookieRecord] {
        let b = [UInt8](data)
        guard b.count >= 8, b[0..<4] == [0x63, 0x6F, 0x6F, 0x6B] else { throw CookieImportError("not a Safari Cookies.binarycookies file") }
        let pages = Int(be(b, 4))
        var sizes: [Int] = []
        for i in 0..<pages {
            guard 8 + 4 * i + 4 <= b.count else { throw CookieImportError("the cookie file is cut short") }
            sizes.append(Int(be(b, 8 + 4 * i)))
        }
        var start = 8 + 4 * pages
        var out: [CookieRecord] = []
        for size in sizes {
            guard start + size <= b.count else { throw CookieImportError("the cookie file is cut short") }
            out += try page(Array(b[start..<start + size]))
            start += size
        }
        return out
    }

    static func page(_ p: [UInt8]) throws -> [CookieRecord] {
        guard p.count >= 8 else { throw CookieImportError("a cookie page is cut short") }
        let count = Int(le(p, 4))
        var out: [CookieRecord] = []
        for i in 0..<count {
            guard 8 + 4 * i + 4 <= p.count else { throw CookieImportError("a cookie page is cut short") }
            let at = Int(le(p, 8 + 4 * i))
            guard at + 56 <= p.count else { throw CookieImportError("a cookie is cut short") }
            let size = Int(le(p, at))
            guard at + size <= p.count else { throw CookieImportError("a cookie is cut short") }
            let c = Array(p[at..<at + size])
            let flags = le(c, 8)
            func text(_ offset: Int) -> String {
                let from = Int(le(c, offset))
                guard from < c.count else { return "" }
                return String(decoding: c[from...].prefix { $0 != 0 }, as: UTF8.self)
            }
            let expiry = c.withUnsafeBytes { $0.loadUnaligned(fromByteOffset: 40, as: Double.self) }
            out.append(CookieRecord(
                name: text(20), value: text(28), domain: text(16), path: text(24),
                expires: expiry > 0 ? Date(timeIntervalSince1970: expiry + macEpoch) : nil,
                secure: flags & 1 != 0, httpOnly: flags & 4 != 0
            ))
        }
        return out
    }

    static func be(_ b: [UInt8], _ i: Int) -> UInt32 {
        UInt32(b[i]) << 24 | UInt32(b[i + 1]) << 16 | UInt32(b[i + 2]) << 8 | UInt32(b[i + 3])
    }

    static func le(_ b: [UInt8], _ i: Int) -> UInt32 {
        UInt32(b[i]) | UInt32(b[i + 1]) << 8 | UInt32(b[i + 2]) << 16 | UInt32(b[i + 3]) << 24
    }
}

public enum CookieImport {
    public static func site(of domain: String) -> String {
        let parts = domain.trimmingCharacters(in: CharacterSet(charactersIn: ".")).split(separator: ".")
        return parts.suffix(2).joined(separator: ".")
    }

    public static func summary(_ cookies: [CookieRecord]) -> [DomainCount] {
        var counts: [String: Int] = [:]
        for c in cookies { counts[site(of: c.domain), default: 0] += 1 }
        return counts.map { DomainCount(domain: $0.key, count: $0.value) }.sorted { $0.domain < $1.domain }
    }

    public static func only(_ cookies: [CookieRecord], domains: Set<String>) -> [CookieRecord] {
        cookies.filter { domains.contains(site(of: $0.domain)) }
    }
}

public struct CookieBrowser: Sendable, Equatable {
    public var name: String
    public var root: String
    public var keychainService: String?

    public static let all: [CookieBrowser] = [
        CookieBrowser(name: "Chrome", root: "Library/Application Support/Google/Chrome", keychainService: "Chrome Safe Storage"),
        CookieBrowser(name: "Arc", root: "Library/Application Support/Arc/User Data", keychainService: "Arc Safe Storage"),
        CookieBrowser(name: "Brave", root: "Library/Application Support/BraveSoftware/Brave-Browser", keychainService: "Brave Safe Storage"),
        CookieBrowser(name: "Edge", root: "Library/Application Support/Microsoft Edge", keychainService: "Microsoft Edge Safe Storage"),
    ]

    public static let safari = CookieBrowser(name: "Safari", root: "Library/Containers/com.apple.Safari/Data/Library/Cookies", keychainService: nil)
}

public struct CookieProfile: Sendable, Equatable, Identifiable {
    public var id: String { file.path }
    public var browser: CookieBrowser
    public var profile: String
    public var file: URL

    public var label: String { profile.isEmpty ? browser.name : browser.name + " · " + profile }
}

public enum CookieSources {
    public static func profiles(home: URL) -> [CookieProfile] {
        let fm = FileManager.default
        var out: [CookieProfile] = []
        for browser in CookieBrowser.all {
            let root = home.appendingPathComponent(browser.root)
            let names = ((try? fm.contentsOfDirectory(atPath: root.path)) ?? []).sorted()
            for name in names {
                for file in ["Network/Cookies", "Cookies"] {
                    let url = root.appendingPathComponent(name).appendingPathComponent(file)
                    if fm.fileExists(atPath: url.path) {
                        out.append(CookieProfile(browser: browser, profile: name, file: url))
                        break
                    }
                }
            }
        }
        let safari = home.appendingPathComponent(CookieBrowser.safari.root).appendingPathComponent("Cookies.binarycookies")
        if fm.fileExists(atPath: safari.path) {
            out.append(CookieProfile(browser: .safari, profile: "", file: safari))
        }
        return out
    }
}
