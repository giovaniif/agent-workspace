import Foundation
import Synchronization

public enum Endpoint: Sendable, Equatable {
    case local(binary: String)
    case ssh(host: String, remoteBinary: String)

    public var argv: [String] {
        switch self {
        case let .local(binary):
            [binary, "rpc"]
        case let .ssh(host, remoteBinary):
            ["ssh", "-T", "-o", "BatchMode=yes", "-o", "ServerAliveInterval=15", host, remoteBinary, "rpc"]
        }
    }
}

public final class LineProcess: Sendable {
    public let lines: AsyncStream<Data>
    private let process: Mutex<Process>
    private let input: FileHandle

    public init(argv: [String], environment: [String: String]? = nil) throws {
        signal(SIGPIPE, SIG_IGN)
        let process = Process()
        let stdin = Pipe()
        let stdout = Pipe()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = argv
        if let environment { process.environment = environment }
        process.standardInput = stdin
        process.standardOutput = stdout
        process.standardError = FileHandle.nullDevice
        let (stream, continuation) = AsyncStream<Data>.makeStream()
        lines = stream
        input = stdin.fileHandleForWriting
        try process.run()
        self.process = Mutex(process)
        let output = stdout.fileHandleForReading
        Thread.detachNewThread {
            var buffer = Data()
            while true {
                let chunk = output.availableData
                if chunk.isEmpty { break }
                buffer.append(chunk)
                while let newline = buffer.firstIndex(of: 0x0A) {
                    continuation.yield(Data(buffer[buffer.startIndex..<newline]))
                    buffer.removeSubrange(buffer.startIndex...newline)
                }
            }
            if !buffer.isEmpty { continuation.yield(buffer) }
            continuation.finish()
        }
    }

    public func send(_ line: Data) throws {
        try process.withLock { _ in
            try input.write(contentsOf: line + Data([0x0A]))
        }
    }

    public func close() {
        process.withLock { process in
            try? input.close()
            if process.isRunning { process.terminate() }
        }
    }
}

public final class RPCClient: Sendable {
    private struct Calls {
        var next: UInt64 = 1
        var pending: [UInt64: CheckedContinuation<Data, Error>] = [:]
        var ended: AgentwsError?
    }

    private struct Header: Decodable {
        var id: UInt64
        var error: RPCError?
    }

    private let process: LineProcess
    private let build: String
    private let calls = Mutex(Calls())

    public init(endpoint: Endpoint, build: String, environment: [String: String]? = nil) throws {
        process = try LineProcess(argv: endpoint.argv, environment: environment)
        self.build = build
        let lines = process.lines
        Task.detached { [self] in
            for await line in lines {
                guard let header = try? JSONDecoder().decode(Header.self, from: line) else { continue }
                if header.id == 0, let error = header.error {
                    end(with: .rpc(error))
                    continue
                }
                let waiter = calls.withLock { $0.pending.removeValue(forKey: header.id) }
                if let error = header.error {
                    waiter?.resume(throwing: AgentwsError.rpc(error))
                } else {
                    waiter?.resume(returning: line)
                }
            }
            end(with: .disconnected)
        }
    }

    public var isClosed: Bool { calls.withLock { $0.ended != nil } }

    public func call<Params: Encodable & Sendable, Result: Decodable>(_ method: String, params: Params) async throws -> Result {
        let line = try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Data, Error>) in
            let id: UInt64? = calls.withLock { state in
                if state.ended != nil { return nil }
                let id = state.next
                state.next += 1
                state.pending[id] = continuation
                return id
            }
            guard let id else {
                continuation.resume(throwing: calls.withLock { $0.ended } ?? AgentwsError.disconnected)
                return
            }
            do {
                try process.send(Request(id: id, method: method, params: params, build: build).encoded())
            } catch {
                let waiter = calls.withLock { $0.pending.removeValue(forKey: id) }
                waiter?.resume(throwing: calls.withLock { $0.ended } ?? AgentwsError.disconnected)
            }
        }
        do {
            return try JSONDecoder().decode(ResultOf<Result>.self, from: line).result
        } catch {
            throw AgentwsError.badReply(String(describing: error))
        }
    }

    public func close() {
        process.close()
    }

    private func end(with error: AgentwsError) {
        let waiters = calls.withLock { state in
            if state.ended == nil { state.ended = error }
            let waiters = state.pending.values
            state.pending = [:]
            return Array(waiters)
        }
        let reason = calls.withLock { $0.ended } ?? error
        for waiter in waiters { waiter.resume(throwing: reason) }
    }
}
