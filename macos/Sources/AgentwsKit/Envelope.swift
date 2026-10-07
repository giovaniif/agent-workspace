import Foundation

public enum ErrorKind: Sendable, Equatable {
    case unsupportedVersion
    case unknownMethod
    case badRequest
    case notFound
    case unavailable
    case failed
    case launchFailed
    case versionMismatch
    case unauthorized
    case rateLimited
    case stale
    case other(String)

    init(code: String) {
        switch code {
        case "unsupported_version": self = .unsupportedVersion
        case "unknown_method": self = .unknownMethod
        case "bad_request": self = .badRequest
        case "not_found": self = .notFound
        case "unavailable": self = .unavailable
        case "failed": self = .failed
        case "launch_failed": self = .launchFailed
        case "version_mismatch": self = .versionMismatch
        case "unauthorized": self = .unauthorized
        case "rate_limited": self = .rateLimited
        case "stale": self = .stale
        default: self = .other(code)
        }
    }
}

public struct RPCError: Codable, Sendable, Equatable, Error {
    public var code: String
    public var message: String

    public init(code: String, message: String) {
        self.code = code
        self.message = message
    }

    public var kind: ErrorKind { ErrorKind(code: code) }
}

public enum AgentwsError: Error, Sendable, Equatable {
    case rpc(RPCError)
    case disconnected
    case badReply(String)
}

public struct Request<Params: Encodable & Sendable>: Encodable, Sendable {
    public var v = AgentwsProtocol.version
    public var id: UInt64
    public var method: String
    public var params: Params
    public var build: String

    public init(id: UInt64, method: String, params: Params, build: String) {
        self.id = id
        self.method = method
        self.params = params
        self.build = build
    }

    public func encoded() throws -> Data {
        try JSONEncoder().encode(self)
    }
}

public struct Response: Decodable, Sendable {
    public var v: Int
    public var id: UInt64
    public var error: RPCError?
    public var build: String?
}

struct ResultOf<R: Decodable>: Decodable {
    var result: R
}

struct ViewReply: Decodable {
    var id: UInt64
    var result: ViewState?
    var diff: ViewDiff?
    var error: RPCError?
}
