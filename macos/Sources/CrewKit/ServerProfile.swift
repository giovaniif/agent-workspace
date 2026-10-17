import Foundation
import Observation

public struct ServerCookie: Codable, Sendable, Equatable {
    public var name: String
    public var value: String
    public var domain: String
    public var path: String
    public var expires: Double?
    public var secure: Bool
    public var httpOnly: Bool

    public init(name: String, value: String, domain: String, path: String, expires: Double?, secure: Bool, httpOnly: Bool) {
        self.name = name
        self.value = value
        self.domain = domain
        self.path = path
        self.expires = expires
        self.secure = secure
        self.httpOnly = httpOnly
    }
}

struct ServerCookiesParams: Encodable, Sendable {
    var cookies: [ServerCookie]
}

struct ServerCookiesResult: Decodable, Sendable {
    var imported: Int
    var total: Int
}

public enum ServerProfile {
    public static func cookies(_ records: [CookieRecord], domains: Set<String>) -> [ServerCookie] {
        CookieImport.only(records, domains: domains).filter { !$0.name.isEmpty && !$0.domain.isEmpty }.map {
            ServerCookie(
                name: $0.name, value: $0.value, domain: $0.domain, path: $0.path.isEmpty ? "/" : $0.path,
                expires: $0.expires?.timeIntervalSince1970, secure: $0.secure, httpOnly: $0.httpOnly
            )
        }
    }
}

@MainActor
@Observable
public final class ServerProfileSend {
    public private(set) var status: String?
    public private(set) var sending = false
    private let caller: Caller

    public init(caller: Caller) {
        self.caller = caller
    }

    @discardableResult
    public func send(_ records: [CookieRecord], domains: Set<String>) async -> Bool {
        let cookies = ServerProfile.cookies(records, domains: domains)
        guard !cookies.isEmpty else {
            status = "Select a site with cookies."
            return false
        }
        sending = true
        defer { sending = false }
        do {
            let result: ServerCookiesResult = try await caller.call("browser.cookies.import", params: ServerCookiesParams(cookies: cookies))
            status = "Sent \(result.imported) cookies to the server. It keeps \(result.total)."
            return true
        } catch CrewError.rpc(let error) where error.kind == .unknownMethod {
            status = "This server has no headless browser. Update the server."
        } catch {
            status = ReviewController.message(error)
        }
        return false
    }
}

public struct HeadlessFrame: Sendable, Equatable {
    public var session: String
    public var png: Data
    public var width: Int
    public var height: Int
}

struct HeadlessViewParams: Encodable, Sendable {
    var session: String
}

struct HeadlessViewResult: Decodable, Sendable {
    var live: Bool
    var png: String?
    var width: Int?
    var height: Int?
}

@MainActor
@Observable
public final class HeadlessView {
    public static let interval: Duration = .seconds(1)
    public private(set) var frame: HeadlessFrame?
    private let caller: Caller

    public init(caller: Caller) {
        self.caller = caller
    }

    public func frame(for session: String) -> HeadlessFrame? {
        frame?.session == session ? frame : nil
    }

    public func refresh(session: String) async {
        do {
            let result: HeadlessViewResult = try await caller.call("browser.headless.view", params: HeadlessViewParams(session: session))
            guard result.live, let text = result.png, let png = Data(base64Encoded: text) else {
                frame = nil
                return
            }
            frame = HeadlessFrame(session: session, png: png, width: result.width ?? 0, height: result.height ?? 0)
        } catch {
            frame = nil
        }
    }

    public func clear() {
        frame = nil
    }
}
