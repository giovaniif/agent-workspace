import Foundation
import Observation

public enum SwitchKind: String, Sendable, Codable {
    case model
    case effort
}

struct SessionRef: Encodable, Sendable {
    var id: String
}

struct RenameParams: Encodable, Sendable {
    var id: String
    var name: String
}

struct MuteParams: Encodable, Sendable {
    var id: String
    var muted: Bool
}

struct SwitchParams: Encodable, Sendable {
    var sessionID: String
    var kind: SwitchKind
    var value: String

    enum CodingKeys: String, CodingKey {
        case sessionID = "session_id", kind, value
    }
}

@MainActor
@Observable
public final class SessionCommands {
    public static let noDevServers = "No dev servers are running in this session's worktrees."

    public var message: String?
    private let caller: Caller

    public init(caller: Caller) {
        self.caller = caller
    }

    public func rename(_ id: String, to name: String) async {
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else { return }
        await run("session.rename", RenameParams(id: id, name: name))
    }

    public func toggleMute(_ session: Session) async {
        await run("session.mute", MuteParams(id: session.id, muted: !session.muted))
    }

    public func choices(for session: Session, kind: SwitchKind) async -> [String] {
        do {
            let options: SessionOptions = try await caller.call("session.options", params: [String: String]())
            guard let harness = options.harnesses.first(where: { $0.harness == session.harness }) else { return [] }
            return kind == .model ? harness.models : harness.efforts
        } catch {
            message = Self.describe(error)
            return []
        }
    }

    public func switchTo(_ id: String, kind: SwitchKind, value: String) async {
        await run("session.switch", SwitchParams(sessionID: id, kind: kind, value: value))
    }

    public func end(_ id: String) async {
        await run("session.end", SessionRef(id: id))
    }

    public func resume(_ id: String) async {
        await run("session.resume", SessionRef(id: id))
    }

    public static func devServers(of session: String, in state: ViewState) -> [Int] {
        let pgids = state.worktrees.filter { $0.sessionID == session }.flatMap { $0.ports ?? [] }.map(\.pgid)
        return Array(Set(pgids)).sorted()
    }

    public func killDevServers(_ id: String, state: ViewState) async {
        let pgids = Self.devServers(of: id, in: state)
        guard !pgids.isEmpty else {
            message = Self.noDevServers
            return
        }
        await run("ports.kill", PortsKillParams(pgids: pgids))
    }

    private func run<Params: Encodable & Sendable>(_ method: String, _ params: Params) async {
        do {
            let _: Ignored = try await caller.call(method, params: params)
            message = nil
        } catch {
            message = Self.describe(error)
        }
    }

    static func describe(_ error: Error) -> String {
        switch error {
        case let AgentwsError.rpc(e): e.message
        case AgentwsError.disconnected: "Not connected to the daemon."
        default: "\(error)"
        }
    }
}
