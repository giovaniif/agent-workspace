import Foundation
import Testing
@testable import CrewKit

struct CookieImportTests {
    static let dir = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().appendingPathComponent("testdata/cookies")

    func fixture(_ name: String) throws -> Data {
        try Data(contentsOf: Self.dir.appendingPathComponent(name))
    }

    let sid = CookieRecord(name: "sid", value: "abc123", domain: ".example.com", path: "/", expires: Date(timeIntervalSince1970: 1893456000), secure: true, httpOnly: true)

    @Test func aNetscapeFileGivesEachCookieAndSkipsCommentsAndBadLines() throws {
        let cookies = try CookieFile.parse(fixture("cookies.txt"))
        #expect(cookies.map(\.name) == ["sid", "token", "pref"])
        #expect(cookies[0] == CookieRecord(name: "sid", value: "abc123", domain: ".example.com", path: "/", expires: Date(timeIntervalSince1970: 1893456000), secure: true, httpOnly: false))
        #expect(cookies[1] == CookieRecord(name: "token", value: "xyz", domain: "app.example.com", path: "/api", expires: nil, secure: false, httpOnly: true))
    }

    @Test func aJSONCookieListIsRead() throws {
        let cookies = try CookieFile.parse(fixture("cookies.json"))
        #expect(cookies.count == 2)
        #expect(cookies[0].name == "sid")
        #expect(cookies[0].domain == ".example.com")
        #expect(cookies[0].secure && cookies[0].httpOnly)
        #expect(cookies[0].expires == Date(timeIntervalSince1970: 1893456000.5))
        #expect(cookies[1].expires == nil)
    }

    @Test func aPlaywrightStorageStateIsRead() throws {
        let cookies = try CookieFile.parse(fixture("storage-state.json"))
        #expect(cookies.first == sid)
        #expect(cookies.last?.expires == nil)
        #expect(cookies.last?.domain == "app.example.com")
    }

    @Test func aFileThatIsNoCookieFormatIsRefused() {
        #expect(throws: CookieImportError.self) { try CookieFile.parse(Data("hello".utf8)) }
        #expect(throws: CookieImportError.self) { try CookieFile.parse(Data(#"{"other":1}"#.utf8)) }
    }

    @Test func safariBinaryCookiesAreRead() throws {
        let cookies = try BinaryCookies.parse(fixture("Cookies.binarycookies"))
        #expect(cookies.count == 2)
        #expect(cookies[0] == sid)
        #expect(cookies[1].name == "pref")
        #expect(cookies[1].domain == "other.dev")
        #expect(!cookies[1].secure)
    }

    @Test func aBrokenBinaryCookiesFileIsRefused() {
        #expect(throws: CookieImportError.self) { try BinaryCookies.parse(Data("cook".utf8)) }
        #expect(throws: CookieImportError.self) { try BinaryCookies.parse(Data("nope1234".utf8)) }
    }

    @Test func theSummaryCountsCookiesForEachDomainAndTheFilterKeepsTheChosenOnes() throws {
        let cookies = try CookieFile.parse(fixture("cookies.txt"))
        #expect(CookieImport.summary(cookies) == [
            DomainCount(domain: "example.com", count: 2),
            DomainCount(domain: "other.dev", count: 1),
        ])
        #expect(CookieImport.only(cookies, domains: ["example.com"]).map(\.name) == ["sid", "token"])
        #expect(CookieImport.site(of: ".app.example.com") == "example.com")
        #expect(CookieImport.site(of: "localhost") == "localhost")
    }

    @Test func browserProfilesAreFoundUnderTheHomeFolder() throws {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent("crew-cookies-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: home) }
        let chrome = home.appendingPathComponent("Library/Application Support/Google/Chrome")
        for (profile, file) in [("Default", "Cookies"), ("Profile 1", "Network/Cookies"), ("Empty", "")] {
            let dir = chrome.appendingPathComponent(profile)
            try FileManager.default.createDirectory(at: dir.appendingPathComponent("Network"), withIntermediateDirectories: true)
            if !file.isEmpty { try Data().write(to: dir.appendingPathComponent(file)) }
        }
        let safari = home.appendingPathComponent("Library/Containers/com.apple.Safari/Data/Library/Cookies")
        try FileManager.default.createDirectory(at: safari, withIntermediateDirectories: true)
        try Data().write(to: safari.appendingPathComponent("Cookies.binarycookies"))
        let found = CookieSources.profiles(home: home)
        #expect(found.map(\.label) == ["Chrome · Default", "Chrome · Profile 1", "Safari"])
        #expect(found[1].file.path.hasSuffix("Profile 1/Network/Cookies"))
        #expect(found[0].browser.keychainService == "Chrome Safe Storage")
        #expect(found[2].browser.keychainService == nil)
    }

#if canImport(CommonCrypto) && canImport(SQLite3)
    struct Vectors: Decodable {
        struct Vector: Decodable { var host: String; var version: Int; var hex: String; var value: String }
        var password: String
        var cookies: [Vector]
    }

    @Test func chromeValuesDecryptWithTheSafeStorageKey() throws {
        let vectors = try JSONDecoder().decode(Vectors.self, from: fixture("chrome-vectors.json"))
        let key = ChromeCrypto.key(password: vectors.password)
        for v in vectors.cookies {
            let data = Data(stride(from: 0, to: v.hex.count, by: 2).map { i in
                UInt8(v.hex[v.hex.index(v.hex.startIndex, offsetBy: i)...].prefix(2), radix: 16)!
            })
            #expect(try ChromeCrypto.decrypt(data, key: key, host: v.host, version: v.version) == v.value)
        }
        #expect(throws: CookieImportError.self) { try ChromeCrypto.decrypt(Data("v10short".utf8), key: key, host: "x", version: 24) }
    }

    @Test func aChromeCookieDatabaseIsReadAndDecrypted() throws {
        let vectors = try JSONDecoder().decode(Vectors.self, from: fixture("chrome-vectors.json"))
        let cookies = try ChromeCookies.read(file: Self.dir.appendingPathComponent("Chrome-Cookies"), password: vectors.password)
        #expect(cookies.count == 2)
        #expect(cookies.first { $0.name == "sid" } == sid)
        #expect(cookies.first { $0.name == "pref" }?.value == "dark")
        #expect(cookies.first { $0.name == "pref" }?.expires == nil)
    }
#endif
}
