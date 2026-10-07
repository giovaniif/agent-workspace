import Foundation
import Synchronization

public struct ControlReply: Equatable, Sendable {
    public var number: Int
    public var ok: Bool
    public var lines: [String]

    public init(number: Int, ok: Bool, lines: [String]) {
        self.number = number
        self.ok = ok
        self.lines = lines
    }
}

public final class TerminalLink: Sendable {
    private typealias Waiter = CheckedContinuation<ControlReply, Error>?

    private struct State: Sendable {
        var matcher = ReplyMatcher<Waiter>()
        var ended = false
    }

    public let events: AsyncStream<ControlEvent>
    private let process: LineProcess
    private let state = Mutex(State())

    public init(argv: [String], environment: [String: String]? = nil) throws {
        process = try LineProcess(argv: argv, environment: environment)
        let (stream, continuation) = AsyncStream.makeStream(of: ControlEvent.self, bufferingPolicy: .unbounded)
        events = stream
        let lines = process.lines
        Task.detached { [self] in
            var parser = ControlParser()
            for await line in lines {
                guard let event = parser.feed(Array(line)) else { continue }
                switch event {
                case .begin:
                    _ = state.withLock { $0.matcher.receive(event) }
                case .reply:
                    let matched = state.withLock { $0.matcher.receive(event) }
                    continuation.yield(event)
                    if let matched, let waiter = matched.call {
                        waiter.resume(returning: ControlReply(number: matched.number, ok: matched.ok, lines: matched.lines))
                    }
                default:
                    continuation.yield(event)
                }
            }
            let waiters = state.withLock { state in
                state.ended = true
                return state.matcher.drain()
            }
            for waiter in waiters { waiter?.resume(throwing: AgentwsError.disconnected) }
            continuation.finish()
            process.close()
        }
    }

    public func command(_ line: String) async throws -> ControlReply {
        try await withCheckedThrowingContinuation { (waiter: CheckedContinuation<ControlReply, Error>) in
            if !enqueue(line, waiter) { waiter.resume(throwing: AgentwsError.disconnected) }
        }
    }

    public func post(_ line: String) {
        _ = enqueue(line, nil)
    }

    private func enqueue(_ line: String, _ waiter: Waiter) -> Bool {
        state.withLock { state in
            guard !state.ended else { return false }
            do {
                try process.send(Data(line.utf8))
            } catch {
                return false
            }
            state.matcher.sent(waiter)
            return true
        }
    }

    public func close() {
        process.close()
    }

    deinit {
        process.close()
    }
}
