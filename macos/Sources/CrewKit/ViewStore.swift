import Foundation
import Observation

public struct Backoff: Sendable, Equatable {
    public var first: Duration
    public var limit: Duration
    private var current: Duration?

    public init(first: Duration = .seconds(1), limit: Duration = .seconds(30)) {
        self.first = first
        self.limit = limit
    }

    public mutating func next() -> Duration {
        let delay = current.map { min($0 * 2, limit) } ?? first
        current = delay
        return delay
    }

    public mutating func reset() {
        current = nil
    }
}

public enum ConnectionStatus: Sendable, Equatable {
    case idle
    case connecting
    case live
    case reconnecting(in: Duration)
    case unavailable(String)
    case versionMismatch(String)
}

@MainActor
@Observable
public final class ViewStore {
    public private(set) var state: ViewState?
    public private(set) var connection: ConnectionStatus = .idle

    public var callsRestarted: @MainActor () -> Void = {}
    public let endpoint: Endpoint
    public let build: String
    private let environment: [String: String]?
    private let sleep: @Sendable (Duration) async throws -> Void
    private var task: Task<Void, Never>?
    private var subscription: LineProcess?
    private var calls: RPCClient?

    private enum Outcome {
        case retry
        case stop
    }

    public init(
        endpoint: Endpoint,
        build: String,
        environment: [String: String]? = nil,
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.endpoint = endpoint
        self.build = build
        self.environment = environment
        self.sleep = sleep
    }

    public func start() {
        guard task == nil else { return }
        task = Task { await run() }
    }

    public func stop() {
        task?.cancel()
        task = nil
        subscription?.close()
        subscription = nil
        calls?.close()
        calls = nil
        connection = .idle
    }

    public func call<Params: Encodable & Sendable, Result: Decodable & Sendable>(_ method: String, params: Params) async throws -> Result {
        let client: RPCClient
        if let calls, !calls.isClosed {
            client = calls
        } else {
            let restarted = calls != nil
            client = try RPCClient(endpoint: endpoint, build: build, environment: environment)
            calls = client
            if restarted { callsRestarted() }
        }
        return try await client.call(method, params: params)
    }

    private func run() async {
        var backoff = Backoff()
        while !Task.isCancelled {
            if await subscribeOnce(&backoff) == .stop {
                task = nil
                return
            }
            if Task.isCancelled { return }
            let delay = backoff.next()
            if case .unavailable = connection {} else { connection = .reconnecting(in: delay) }
            do { try await sleep(delay) } catch { return }
        }
    }

    private func subscribeOnce(_ backoff: inout Backoff) async -> Outcome {
        if case .unavailable = connection {} else { connection = .connecting }
        let process: LineProcess
        do {
            process = try LineProcess(argv: endpoint.argv, environment: environment)
        } catch {
            return .retry
        }
        subscription = process
        defer {
            process.close()
            if subscription === process { subscription = nil }
        }
        do {
            try process.send(Request(id: 1, method: "view.subscribe", params: [String: String](), build: build).encoded())
        } catch {
            return .retry
        }
        for await line in process.lines {
            guard let reply = try? JSONDecoder().decode(ViewReply.self, from: line) else { continue }
            if let error = reply.error {
                switch error.kind {
                case .versionMismatch:
                    connection = .versionMismatch(error.message)
                    return .stop
                case .unavailable:
                    connection = .unavailable(error.message)
                    return .retry
                default:
                    return .retry
                }
            }
            if let result = reply.result {
                state = result
                connection = .live
                backoff.reset()
            } else if let diff = reply.diff {
                state?.apply(diff)
            }
        }
        return .retry
    }
}
