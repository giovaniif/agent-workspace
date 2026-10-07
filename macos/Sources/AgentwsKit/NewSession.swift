import Foundation
import Observation

@MainActor
public protocol Calling: AnyObject {
    func call<Params: Encodable & Sendable, Answer: Decodable & Sendable>(_ method: String, params: Params) async throws -> Answer
}

extension ViewStore: Calling {}

public struct HarnessOptions: Codable, Sendable, Equatable {
    public var harness: String
    public var name: String
    public var tag: String
    @NullAsEmpty public var models: [String]
    @NullAsEmpty public var efforts: [String]
    public var model: String
    public var effort: String
}

public struct SessionOptions: Codable, Sendable, Equatable {
    @NullAsEmpty public var harnesses: [HarnessOptions]
    public var maxParallel: Int

    enum CodingKeys: String, CodingKey {
        case harnesses, maxParallel = "max_parallel"
    }
}

public struct WorkItemResolved: Codable, Sendable, Equatable {
    public var source: String
    public var ref: String?
    public var title: String?
    public var worktree: String
    public var workspace: String
}

public struct NewSessionParams: Encodable, Sendable, Equatable {
    public var workspace: String
    public var workItem: String
    public var harness: String
    public var model: String
    public var effort: String

    enum CodingKeys: String, CodingKey {
        case workspace, workItem = "work_item", harness, model, effort
    }
}

struct ResolveParams: Encodable, Sendable {
    var workspace: String
    var workItem: String

    enum CodingKeys: String, CodingKey {
        case workspace, workItem = "work_item"
    }
}

public struct LauncherEnqueueParams: Encodable, Sendable, Equatable {
    public var workspace: String
    public var input: String
    public var harness: String
    public var model: String
    public var effort: String
}

public struct LauncherEnqueued: Decodable, Sendable, Equatable {
    @NullAsEmpty public var queued: [String]
    @NullAsEmpty public var rejected: [String]
}

struct StartedSession: Decodable, Sendable {
    var id: String

    enum CodingKeys: String, CodingKey {
        case id = "ID"
    }
}

struct SessionID: Encodable, Sendable {
    var id: String
}

struct Ignored: Decodable, Sendable {}

public struct WorkspaceChoice: Sendable, Equatable, Identifiable {
    public var id: String { root }
    public var root: String
    public var name: String
    public var detail: String

    init(_ workspace: Workspace) {
        root = workspace.root
        name = URL(fileURLWithPath: workspace.root).lastPathComponent
        detail = workspace.kind == "orchestration" ? "orchestration root · \(workspace.repos.count) repos" : "single repo"
    }
}

public struct QuotaWarning: Sendable, Equatable {
    public var message: String
    public var button: String?
    public var switchTo: String?
}

public struct WorkItemCard: Sendable, Equatable {
    public var source: String
    public var title: String
    public var branch: String
    public var existing: String?

    public init(source: String, title: String, branch: String, existing: String?) {
        self.source = source
        self.title = title
        self.branch = branch
        self.existing = existing
    }

    init(_ resolved: WorkItemResolved, worktrees: [Worktree]) {
        let ref = resolved.ref ?? ""
        switch resolved.source {
        case "linear": source = ["Linear", ref].filter { !$0.isEmpty }.joined(separator: " ")
        case "pr": source = ["PR", ref].filter { !$0.isEmpty }.joined(separator: " ")
        default: source = "Text"
        }
        title = resolved.title ?? ""
        branch = resolved.worktree
        existing = worktrees.first { $0.branch == resolved.worktree }?.path
    }
}

public struct LaunchRow: Sendable, Equatable, Identifiable {
    public var id: String
    public var ref: String
    public var harness: String
    public var status: String

    public init(id: String, ref: String, harness: String, status: String) {
        self.id = id
        self.ref = ref
        self.harness = harness
        self.status = status
    }

    public static func rows(_ queue: [JSONValue]) -> [LaunchRow] {
        queue.compactMap { value in
            guard case let .object(item) = value else { return nil }
            var harness = ""
            if case let .object(request)? = item["Request"] {
                let name = request["Harness"]?.string ?? ""
                harness = name == "codex" ? "CX" : name == "claude" ? "CC" : name.uppercased()
            }
            let err = item["Err"]?.string ?? ""
            let status = !err.isEmpty ? "failed: " + err : (item["Starting"]?.bool ?? false) ? "starting" : "queued"
            return LaunchRow(id: item["ID"]?.string ?? "", ref: item["Ref"]?.string ?? "", harness: harness, status: status)
        }
    }
}

public struct NewSessionForm: Sendable, Equatable {
    public var workspaces: [WorkspaceChoice] = []
    public var workspace = ""
    public var workItem = ""
    public var harnesses: [HarnessOptions] = []
    public private(set) var harness = ""
    public var model = ""
    public var effort = ""
    public var launchInput = ""

    public init() {}

    init(workspaces: [Workspace], options: SessionOptions) {
        let ordered = workspaces.enumerated().sorted { a, b in
            let at = Timestamp.parse(a.element.lastUsed) ?? .distantPast
            let bt = Timestamp.parse(b.element.lastUsed) ?? .distantPast
            return at != bt ? at > bt : a.offset < b.offset
        }.map(\.element)
        self.workspaces = ordered.map(WorkspaceChoice.init)
        workspace = ordered.first?.root ?? ""
        harnesses = options.harnesses
        if let first = options.harnesses.first { choose(harness: first.harness) }
    }

    public var chosen: HarnessOptions? { harnesses.first { $0.harness == harness } }

    public mutating func choose(harness: String) {
        self.harness = harness
        model = chosen?.model ?? ""
        effort = chosen?.effort ?? ""
    }

    public var trimmedWorkItem: String { workItem.trimmingCharacters(in: .whitespacesAndNewlines) }

    public var canStart: Bool { !trimmedWorkItem.isEmpty && !harness.isEmpty }

    public var newSession: NewSessionParams {
        NewSessionParams(workspace: workspace, workItem: trimmedWorkItem, harness: harness, model: model, effort: effort)
    }

    public var enqueue: LauncherEnqueueParams {
        LauncherEnqueueParams(workspace: workspace, input: launchInput, harness: harness, model: model, effort: effort)
    }

    public func warning(limits: [JSONValue]) -> QuotaWarning? {
        let quotas = limits.compactMap { value -> (harness: String, label: String, left: Int, low: Bool)? in
            guard case let .object(q) = value else { return nil }
            return (q["Harness"]?.string ?? "", q["label"]?.string ?? "", Int(q["LeftPercent"]?.number ?? 0), q["low"]?.bool ?? false)
        }
        guard let low = quotas.filter({ $0.harness == harness && $0.low }).min(by: { $0.left < $1.left }) else { return nil }
        let name = chosen?.name ?? harness
        let message = "\(name) \(low.label) quota is low: \(low.left)% left"
        let other = harnesses.first { h in
            let theirs = quotas.filter { $0.harness == h.harness }
            return h.harness != harness && !theirs.isEmpty && !theirs.contains { $0.low }
        }
        guard let other else { return QuotaWarning(message: message, button: nil, switchTo: nil) }
        return QuotaWarning(message: message, button: "Switch to \(other.name)", switchTo: other.harness)
    }
}

@MainActor
@Observable
public final class NewSession {
    public var form = NewSessionForm()
    public private(set) var maxParallel = 0
    public private(set) var limits: [JSONValue] = []
    public private(set) var card: WorkItemCard?
    public private(set) var resolveError: String?
    public private(set) var failure: String?
    public private(set) var starting = false
    public private(set) var launchNote: String?
    public private(set) var loadError: String?

    private let caller: Calling

    public init(caller: Calling) {
        self.caller = caller
    }

    public var warning: QuotaWarning? { form.warning(limits: limits) }

    public func load(state: ViewState?) async {
        do {
            let options: SessionOptions = try await caller.call("session.options", params: [String: String]())
            form = NewSessionForm(workspaces: state?.workspaces ?? [], options: options)
            maxParallel = options.maxParallel
            limits = state?.limits ?? []
            loadError = nil
        } catch {
            loadError = Self.message(error)
        }
    }

    public func observe(_ state: ViewState?) {
        limits = state?.limits ?? []
    }

    public func takeSwitch() {
        if let target = warning?.switchTo { form.choose(harness: target) }
    }

    public func resolve(in state: ViewState?) async {
        let item = form.trimmedWorkItem
        let workspace = form.workspace
        guard !item.isEmpty else {
            card = nil
            resolveError = nil
            return
        }
        do {
            let resolved: WorkItemResolved = try await caller.call("session.resolve", params: ResolveParams(workspace: workspace, workItem: item))
            guard item == form.trimmedWorkItem, workspace == form.workspace else { return }
            card = WorkItemCard(resolved, worktrees: state?.worktrees ?? [])
            resolveError = nil
        } catch {
            guard item == form.trimmedWorkItem, workspace == form.workspace else { return }
            card = nil
            resolveError = Self.message(error)
        }
    }

    public func start() async -> String? {
        guard form.canStart, !starting else { return nil }
        starting = true
        failure = nil
        defer { starting = false }
        do {
            let started: StartedSession = try await caller.call("session.new", params: form.newSession)
            let _: Ignored? = try? await caller.call("session.focus", params: SessionID(id: started.id))
            return started.id
        } catch {
            failure = Self.message(error)
            return nil
        }
    }

    public func enqueue() async {
        guard !form.launchInput.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, !form.harness.isEmpty else { return }
        do {
            let result: LauncherEnqueued = try await caller.call("launcher.enqueue", params: form.enqueue)
            var parts = ["Queued \(result.queued.count)"]
            if !result.rejected.isEmpty { parts.append("not a Linear issue: " + result.rejected.joined(separator: ", ")) }
            launchNote = parts.joined(separator: " · ")
            form.launchInput = ""
        } catch {
            launchNote = Self.message(error)
        }
    }

    static func message(_ error: Error) -> String {
        switch error {
        case let AgentwsError.rpc(rpc): rpc.message
        case AgentwsError.disconnected: "The connection to the daemon dropped."
        case let AgentwsError.badReply(text): text
        default: "\(error)"
        }
    }
}
