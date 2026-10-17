import Foundation
import Observation

public struct MateThread: Codable, Sendable, Equatable, Identifiable {
    public var mate: String
    public var team: String
    public var channel: String
    public var threadTS: String
    public var sessionID: String
    public var url: String
    public var session: String
    public var state: String
    public var ended: Bool

    public init(
        mate: String, team: String = "", channel: String, threadTS: String, sessionID: String,
        url: String = "", session: String = "", state: String = "idle", ended: Bool = false
    ) {
        self.mate = mate
        self.team = team
        self.channel = channel
        self.threadTS = threadTS
        self.sessionID = sessionID
        self.url = url
        self.session = session
        self.state = state
        self.ended = ended
    }

    public var id: String { mate + "\n" + channel + "\n" + threadTS }

    public var label: String { session.isEmpty ? sessionID : session }

    enum CodingKeys: String, CodingKey {
        case mate, team, channel, url, session, state, ended
        case threadTS = "thread_ts"
        case sessionID = "session_id"
    }
}

public struct MateRow: Codable, Sendable, Equatable, Identifiable {
    public var name: String
    public var state: String
    public var error: String?
    public var hold: String?
    @NullAsEmpty public var work: [String]
    @NullAsEmpty public var threads: [MateThread]

    public init(name: String, state: String, error: String? = nil, hold: String? = nil, work: [String] = [], threads: [MateThread] = []) {
        self.name = name
        self.state = state
        self.error = error
        self.hold = hold
        self.work = work
        self.threads = threads
    }

    public var id: String { name }

    public var paused: Bool { state == "paused" }

    public var label: String {
        var parts = [state.replacingOccurrences(of: "_", with: " ")]
        if let hold, !hold.isEmpty {
            parts.append("held by " + hold.replacingOccurrences(of: "_", with: " "))
        }
        return parts.joined(separator: " · ")
    }

    public var currentWork: [MateThread] {
        threads.filter { work.contains($0.sessionID) }
    }

    enum CodingKeys: String, CodingKey {
        case name, state, error, work, threads
        case hold = "guard"
    }
}

public struct MateBoard: Codable, Sendable, Equatable {
    public var stopped: Bool
    @NullAsEmpty public var mates: [MateRow]

    public init(stopped: Bool = false, mates: [MateRow] = []) {
        self.stopped = stopped
        self.mates = mates
    }
}

public struct MateDefinition: Codable, Sendable, Equatable, Identifiable {
    public var name: String
    public var prompt: String
    public var agent: String
    public var model: String?
    public var effort: String?
    public var repos: [String]?
    public var owner: String?
    public var enabled: Bool

    public init(name: String, prompt: String, agent: String = "claude", enabled: Bool = true) {
        self.name = name
        self.prompt = prompt
        self.agent = agent
        self.enabled = enabled
    }

    public var id: String { name }
}

public struct MateDefinitions: Decodable, Sendable, Equatable {
    public var mates: [MateDefinition]
    public var problems: [String]

    enum CodingKeys: String, CodingKey { case mates, problems }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        mates = try c.decodeIfPresent([MateDefinition].self, forKey: .mates) ?? []
        problems = try c.decodeIfPresent([String].self, forKey: .problems) ?? []
    }
}

public struct MateSetupLink: Codable, Sendable, Equatable {
    public var manifest: String
    public var url: String
}

public struct MateClaimCode: Codable, Sendable, Equatable {
    public var name: String
    public var code: String
    public var expiresAt: String

    enum CodingKeys: String, CodingKey {
        case name, code
        case expiresAt = "expires_at"
    }
}

public struct ApprovalAction: Codable, Sendable, Equatable {
    public var kind: String
    public var tool: String
    public var input: String

    enum CodingKeys: String, CodingKey {
        case kind = "Kind", tool = "Tool", input = "Input"
    }
}

public struct ApprovalItem: Codable, Sendable, Equatable, Identifiable {
    public var id: String
    public var sessionID: String
    public var mate: String
    public var requester: String
    public var action: ApprovalAction
    public var at: String
    public var state: String

    public var title: String {
        (mate.isEmpty ? "crew" : mate) + ": " + action.kind.replacingOccurrences(of: "_", with: " ")
    }

    enum CodingKeys: String, CodingKey {
        case id = "ID", sessionID = "SessionID", mate = "Mate", requester = "Requester"
        case action = "Action", at = "At", state = "State"
    }
}

public struct MateDraft: Sendable, Equatable {
    public var name = ""
    public var agent = "claude"
    public var model = ""
    public var effort = ""
    public var repos = ""
    public var prompt = ""
    public var owner: String?
    public var enabled = true

    public init() {}

    public init(_ m: MateDefinition) {
        name = m.name
        agent = m.agent
        model = m.model ?? ""
        effort = m.effort ?? ""
        repos = (m.repos ?? []).joined(separator: ", ")
        prompt = m.prompt
        owner = m.owner
        enabled = m.enabled
    }

    public var mate: MateDefinition {
        func value(_ s: String) -> String? {
            let t = s.trimmingCharacters(in: .whitespacesAndNewlines)
            return t.isEmpty ? nil : t
        }
        var m = MateDefinition(name: name.trimmingCharacters(in: .whitespaces), prompt: prompt, agent: agent, enabled: enabled)
        m.model = value(model)
        m.effort = value(effort)
        let list = repos.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        m.repos = list.isEmpty ? nil : list
        m.owner = owner
        return m
    }
}

struct MateNameParams: Encodable, Sendable {
    var name: String
}

struct MateStopAllParams: Encodable, Sendable {
    var stop: Bool
}

struct MateSetParams: Encodable, Sendable {
    var mate: MateDefinition
}

struct MateTokensParams: Encodable, Sendable {
    var name: String
    var botToken: String
    var appToken: String

    enum CodingKeys: String, CodingKey {
        case name
        case botToken = "bot_token"
        case appToken = "app_token"
    }
}

struct ApprovalDecideParams: Encodable, Sendable {
    var id: String
    var approve: Bool
}

@MainActor
@Observable
public final class MatesModel {
    public private(set) var definitions: [MateDefinition] = []
    public private(set) var problems: [String] = []
    public private(set) var approvals: [ApprovalItem] = []
    public private(set) var setup: MateSetupLink?
    public private(set) var claim: MateClaimCode?
    public var message: String?
    private let caller: Caller

    public init(caller: Caller) {
        self.caller = caller
    }

    public func load() async {
        do {
            let list: MateDefinitions = try await caller.call("mate.list", params: EmptyParams())
            definitions = list.mates
            problems = list.problems
            let pending: [ApprovalItem]? = try await caller.call("approval.list", params: EmptyParams())
            approvals = pending ?? []
        } catch {
            message = ReviewController.message(error)
        }
    }

    public func definition(_ name: String) -> MateDefinition? {
        definitions.first { $0.name == name }
    }

    public func setPaused(_ name: String, paused: Bool) async {
        await run(paused ? "mate.pause" : "mate.resume", MateNameParams(name: name))
    }

    public func stopAll(_ stop: Bool) async {
        await run("mate.stop_all", MateStopAllParams(stop: stop))
    }

    public func save(_ draft: MateDraft) async -> Bool {
        guard await run("mate.set", MateSetParams(mate: draft.mate)) else { return false }
        await load()
        return true
    }

    public func startSetup(_ name: String) async {
        claim = nil
        do {
            setup = try await caller.call("mate.setup", params: MateNameParams(name: name))
        } catch {
            message = ReviewController.message(error)
        }
    }

    public func connect(_ name: String, bot: String, app: String) async {
        let tokens = MateTokensParams(
            name: name,
            botToken: bot.trimmingCharacters(in: .whitespacesAndNewlines),
            appToken: app.trimmingCharacters(in: .whitespacesAndNewlines)
        )
        guard await run("mate.set_tokens", tokens) else { return }
        do {
            claim = try await caller.call("mate.claim", params: MateNameParams(name: name))
        } catch {
            message = ReviewController.message(error)
        }
    }

    public func decide(_ id: String, approve: Bool) async {
        let ok = await run("approval.decide", ApprovalDecideParams(id: id, approve: approve))
        let failure = message
        await load()
        if !ok { message = failure }
    }

    @discardableResult
    private func run<Params: Encodable & Sendable>(_ method: String, _ params: Params) async -> Bool {
        do {
            let _: Ignored = try await caller.call(method, params: params)
            message = nil
            return true
        } catch {
            message = ReviewController.message(error)
            return false
        }
    }
}
