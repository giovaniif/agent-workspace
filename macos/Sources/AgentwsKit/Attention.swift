import Foundation
import Observation

public struct Banner: Codable, Sendable, Equatable {
    public var title: String
    public var body: String
    public var state: String
    public var sound: String
    public var group: String
    public var terminal: String

    enum CodingKeys: String, CodingKey {
        case title = "Title", body = "Body", state = "State", sound = "Sound", group = "Group", terminal = "Terminal"
    }
}

public struct Notice: Codable, Sendable, Equatable {
    public var banner: Banner?
    public var remove: String?
    public var focused: Bool?
}

public struct NoticeLine: Decodable, Sendable {
    public var id: UInt64
    public var notice: Notice?
    public var error: RPCError?
}

public struct PromptChoice: Codable, Sendable, Equatable, Identifiable {
    public var id: String
    public var label: String

    public init(id: String, label: String) {
        self.id = id
        self.label = label
    }
}

public struct Prompt: Codable, Sendable, Equatable {
    public var id: String?
    public var text: String
    public var choices: [PromptChoice]
    public var raw: String?

    public init(id: String?, text: String, choices: [PromptChoice], raw: String? = nil) {
        self.id = id
        self.text = text
        self.choices = choices
        self.raw = raw
    }

    public var allow: PromptChoice? { choices.first }

    public var always: PromptChoice? {
        choices.dropFirst().first { $0.label.lowercased().hasPrefix("yes") }
    }
}

public enum BannerKind: Sendable, Equatable {
    case permission
    case waiting
    case other

    public init(state: String) {
        switch state {
        case "permission": self = .permission
        case "waiting": self = .waiting
        default: self = .other
        }
    }
}

public struct BannerPost: Sendable, Equatable {
    public var id: String
    public var title: String
    public var body: String
    public var kind: BannerKind

    public init(id: String, title: String, body: String, kind: BannerKind) {
        self.id = id
        self.title = title
        self.body = body
        self.kind = kind
    }
}

@MainActor
public protocol Notifier: AnyObject {
    func post(_ banner: BannerPost)
    func withdraw(_ id: String)
}

public struct PermissionCard: Sendable, Equatable {
    public var session: String
    public var prompt: Prompt
    public var hidden: Bool

    public init(session: String, prompt: Prompt, hidden: Bool = false) {
        self.session = session
        self.prompt = prompt
        self.hidden = hidden
    }
}

struct ViewingParams: Encodable, Sendable {
    var session: String
    var front: Bool
}

struct AnswerParams: Encodable, Sendable {
    var session: String
    var choice: String
    var prompt: String
}

struct SendParams: Encodable, Sendable {
    var session: String
    var text: String
}

struct SendReply: Decodable, Sendable {}

@MainActor
@Observable
public final class Attention {
    public static let gone = "That prompt is gone; nothing was sent."

    public private(set) var card: PermissionCard?
    public var message: String?
    public private(set) var viewing: String?
    public private(set) var front = false

    private let caller: Caller
    private let notifier: Notifier
    private var reported: ViewingParams?
    private var frontWindow: String?
    private var refreshes = 0

    public init(caller: Caller, notifier: Notifier) {
        self.caller = caller
        self.notifier = notifier
    }

    public func receive(_ notice: Notice) {
        if let id = notice.remove, !id.isEmpty { notifier.withdraw(id) }
        guard let banner = notice.banner, !banner.group.isEmpty else { return }
        if front && viewing == banner.group { return }
        notifier.post(BannerPost(id: banner.group, title: banner.title, body: banner.body, kind: BannerKind(state: banner.state)))
    }

    public func view(session: String?, front: Bool, window: String = "") async {
        if front {
            frontWindow = window
        } else if let frontWindow, frontWindow != window {
            return
        } else {
            frontWindow = nil
        }
        viewing = session
        self.front = front
        if front, let session { notifier.withdraw(session) }
        let params = ViewingParams(session: session ?? "", front: front)
        if let reported, reported.session == params.session, reported.front == params.front { return }
        reported = params
        await report(params)
    }

    public func reconnected() async {
        guard let reported else { return }
        await report(reported)
    }

    private func report(_ params: ViewingParams) async {
        do {
            let _: Empty = try await caller.call("client.viewing", params: params)
        } catch {
            reported = nil
        }
    }

    public func refreshCard(session: String?, state: String?) async {
        refreshes += 1
        let mine = refreshes
        guard let session, state == "permission" else {
            card = nil
            return
        }
        let prompt: Prompt
        do {
            prompt = try await fetchPrompt(session)
        } catch let AgentwsError.rpc(error) where error.kind == .notFound {
            if mine == refreshes { card = nil }
            return
        } catch {
            if mine == refreshes, card?.session != session { card = nil }
            return
        }
        guard mine == refreshes else { return }
        guard !prompt.choices.isEmpty else {
            card = nil
            return
        }
        let hidden = card.map { $0.session == session && $0.prompt.id == prompt.id && $0.hidden } ?? false
        card = PermissionCard(session: session, prompt: prompt, hidden: hidden)
    }

    public func hideCard() {
        card?.hidden = true
    }

    public func answer(choice: String) async {
        guard let card else { return }
        await answer(session: card.session, choice: choice, prompt: card.prompt.id ?? "")
    }

    public func allow(_ session: String, always: Bool = false) async {
        let prompt: Prompt
        do {
            prompt = try await fetchPrompt(session)
        } catch {
            settle(session, message: Self.gone)
            return
        }
        guard let choice = always ? prompt.always : prompt.allow else {
            settle(session, message: Self.gone)
            return
        }
        await answer(session: session, choice: choice.id, prompt: prompt.id ?? "")
    }

    public func reply(_ session: String, text: String) async {
        do {
            let _: SendReply = try await caller.call("session.send", params: SendParams(session: session, text: text))
            notifier.withdraw(session)
        } catch {
            message = Self.describe(error)
        }
    }

    private func fetchPrompt(_ session: String) async throws -> Prompt {
        try await caller.call("session.prompt", params: ["session": session])
    }

    private func answer(session: String, choice: String, prompt: String) async {
        do {
            let _: Empty = try await caller.call("session.answer", params: AnswerParams(session: session, choice: choice, prompt: prompt))
            settle(session, message: nil)
        } catch let AgentwsError.rpc(error) where error.kind == .stale || error.kind == .notFound {
            settle(session, message: Self.gone)
        } catch {
            message = Self.describe(error)
        }
    }

    private func settle(_ session: String, message: String?) {
        if card?.session == session { card = nil }
        notifier.withdraw(session)
        self.message = message
    }

    static func describe(_ error: Error) -> String {
        switch error {
        case let AgentwsError.rpc(e): e.message
        case AgentwsError.disconnected: "Not connected to the daemon."
        default: "\(error)"
        }
    }
}

public struct AttentionRow: Sendable, Equatable, Identifiable {
    public var id: String
    public var name: String
    public var `where`: String
    public var style: StateStyle
    public var permission: Bool
}

public struct AttentionMenu: Sendable, Equatable {
    public var needsYou: [AttentionRow] = []
    public var doneUnread: [AttentionRow] = []
    public var working: [AttentionRow] = []
    public var meters: [QuotaMeter] = []

    public init(state: ViewState?, now: Date) {
        guard let state else { return }
        let live = state.sortedSessions.filter { !$0.ended }
        let row: (Session) -> AttentionRow = {
            AttentionRow(id: $0.id, name: $0.name, where: $0.where, style: StateStyle(state: $0.state), permission: $0.state == "permission")
        }
        needsYou = live.filter { StateStyle.needsYou($0.state) }.map(row)
        doneUnread = live.filter { $0.state == "done" && $0.unread }.map(row)
        working = live.filter { $0.state == "running" }.map(row)
        meters = QuotaMeter.all(state.limits, now: now)
    }

    public var badge: String? { needsYou.isEmpty ? nil : "\(needsYou.count)" }

    public var title: String {
        needsYou.isEmpty && doneUnread.isEmpty ? "" : "\(needsYou.count) · \(doneUnread.count)"
    }
}

@MainActor
public final class NoticeStream {
    private let endpoint: Endpoint
    private let build: String
    private let environment: [String: String]?
    private let sleep: @Sendable (Duration) async throws -> Void
    private let deliver: @MainActor (Notice) -> Void
    private let connected: @MainActor () -> Void
    private var task: Task<Void, Never>?
    private var process: LineProcess?

    public init(
        endpoint: Endpoint,
        build: String,
        environment: [String: String]? = nil,
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) },
        connected: @escaping @MainActor () -> Void = {},
        deliver: @escaping @MainActor (Notice) -> Void
    ) {
        self.endpoint = endpoint
        self.build = build
        self.environment = environment
        self.sleep = sleep
        self.connected = connected
        self.deliver = deliver
    }

    public func start() {
        guard task == nil else { return }
        task = Task { await run() }
    }

    public func stop() {
        task?.cancel()
        task = nil
        process?.close()
        process = nil
    }

    private func run() async {
        var backoff = Backoff()
        while !Task.isCancelled {
            if await once(&backoff) { return }
            if Task.isCancelled { return }
            do { try await sleep(backoff.next()) } catch { return }
        }
    }

    private func once(_ backoff: inout Backoff) async -> Bool {
        guard let line = try? LineProcess(argv: endpoint.argv, environment: environment) else { return false }
        process = line
        defer {
            line.close()
            if process === line { process = nil }
        }
        guard let request = try? Request(id: 1, method: "notify.stream", params: [String: String](), build: build).encoded(),
              (try? line.send(request)) != nil else { return false }
        for await data in line.lines {
            guard let reply = try? JSONDecoder().decode(NoticeLine.self, from: data) else { continue }
            if let error = reply.error {
                return error.kind == .versionMismatch
            }
            if let notice = reply.notice {
                deliver(notice)
            } else {
                backoff.reset()
                connected()
            }
        }
        return false
    }
}

public struct BridgeAgent: Sendable, Equatable {
    public var label: String
    public var host: String
    public var remoteBinary: String?

    public init(label: String, host: String, remoteBinary: String?) {
        self.label = label
        self.host = host
        self.remoteBinary = remoteBinary
    }

    public func removeArgv(binary: String) -> [String] {
        var argv = [binary, "setup", "bridge"]
        if let remoteBinary { argv += ["--remote-bin", remoteBinary] }
        return argv + ["--remove", host]
    }
}

public enum BridgeAgents {
    public static let prefix = "dev.agentws.bridge."

    public static func find(in directory: URL) -> [BridgeAgent] {
        let names = (try? FileManager.default.contentsOfDirectory(atPath: directory.path)) ?? []
        return names.filter { $0.hasPrefix(prefix) && $0.hasSuffix(".plist") }.sorted().compactMap { name in
            guard let data = FileManager.default.contents(atPath: directory.appendingPathComponent(name).path),
                  let plist = try? PropertyListSerialization.propertyList(from: data, format: nil) as? [String: Any],
                  let program = plist["ProgramArguments"] as? [String],
                  let host = program.last, program.count >= 4 else { return nil }
            let remote = program.firstIndex(of: "--remote-bin").flatMap { $0 + 1 < program.count - 1 ? program[$0 + 1] : nil }
            return BridgeAgent(label: plist["Label"] as? String ?? String(name.dropLast(6)), host: host, remoteBinary: remote)
        }
    }
}
