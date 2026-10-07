import Foundation
import Observation

@MainActor
public protocol RPCCalling: AnyObject {
    func call<Params: Encodable & Sendable, Result: Decodable & Sendable>(_ method: String, params: Params) async throws -> Result
}

extension ViewStore: RPCCalling {}

public struct HarnessSetup: Codable, Equatable, Sendable {
    public var installed: Bool
    public var file: String
    public var backup: String?
    public var err: String?

    public init(installed: Bool, file: String, backup: String? = nil, err: String? = nil) {
        self.installed = installed
        self.file = file
        self.backup = backup
        self.err = err
    }
}

public struct NvimSetup: Codable, Equatable, Sendable {
    public var onPath: Bool
    public var configured: Bool
    public var configFile: String
    public var pluginDir: String
    public var pluginFound: Bool

    enum CodingKeys: String, CodingKey {
        case onPath = "on_path"
        case configured
        case configFile = "config_file"
        case pluginDir = "plugin_dir"
        case pluginFound = "plugin_found"
    }

    public init(onPath: Bool, configured: Bool, configFile: String, pluginDir: String, pluginFound: Bool) {
        self.onPath = onPath
        self.configured = configured
        self.configFile = configFile
        self.pluginDir = pluginDir
        self.pluginFound = pluginFound
    }
}

public struct AgentsStatus: Codable, Equatable, Sendable {
    public var done: Bool
    public var harnesses: [String: HarnessSetup]
    public var nvim: NvimSetup

    public init(done: Bool, harnesses: [String: HarnessSetup], nvim: NvimSetup) {
        self.done = done
        self.harnesses = harnesses
        self.nvim = nvim
    }
}

public struct RepoInfo: Codable, Equatable, Sendable {
    public var name: String
    public var path: String
    public var branch: String

    enum CodingKeys: String, CodingKey {
        case name = "Name"
        case path = "Path"
        case branch = "Branch"
    }

    public init(name: String, path: String, branch: String) {
        self.name = name
        self.path = path
        self.branch = branch
    }
}

public struct WorkspaceInfo: Codable, Equatable, Sendable, Identifiable {
    public var root: String
    public var kind: String
    @NullAsEmpty public var repos: [RepoInfo]

    enum CodingKeys: String, CodingKey {
        case root = "Root"
        case kind = "Kind"
        case repos = "Repos"
    }

    public init(root: String, kind: String, repos: [RepoInfo]) {
        self.root = root
        self.kind = kind
        self.repos = repos
    }

    public var id: String { root }

    public var summary: String {
        kind == "orchestration" ? "orchestration root · \(repos.count) repos" : "single repo"
    }
}

struct WorkspaceListReply: Decodable, Sendable {
    var workspaces: [WorkspaceInfo]
    var lastUsed: String

    enum CodingKeys: String, CodingKey {
        case workspaces
        case lastUsed = "last_used"
    }
}

struct Empty: Codable, Sendable {}

@MainActor
@Observable
public final class ServerSettings {
    public private(set) var agents: AgentsStatus?
    public private(set) var workspaces: [WorkspaceInfo] = []
    public private(set) var lastUsed = ""
    public private(set) var error: String?
    public private(set) var notice: String?
    public private(set) var busy = false

    @ObservationIgnored private let caller: any RPCCalling

    public init(caller: any RPCCalling) {
        self.caller = caller
    }

    public func refresh() async {
        await run {
            let status: AgentsStatus = try await self.caller.call("onboarding.status", params: Empty())
            let list: WorkspaceListReply = try await self.caller.call("workspace.list", params: Empty())
            self.agents = status
            self.workspaces = list.workspaces
            self.lastUsed = list.lastUsed
        }
    }

    public func installHooks(_ harness: String) async {
        await change {
            let setup: HarnessSetup = try await self.caller.call("onboarding.install", params: ["harness": harness])
            self.notice = "Hooks installed in \(setup.file)"
        }
    }

    public func removeHooks(_ harness: String) async {
        await change {
            let setup: HarnessSetup = try await self.caller.call("onboarding.remove", params: ["harness": harness])
            self.notice = setup.backup.map { "Hooks removed. Backup: \($0)" } ?? "Hooks removed"
        }
    }

    public func installNvim() async {
        await change {
            let _: NvimSetup = try await self.caller.call("onboarding.nvim", params: Empty())
            self.notice = "nvim plugin set up"
        }
    }

    public func addWorkspace(_ path: String) async {
        await change {
            let added: WorkspaceInfo = try await self.caller.call("workspace.add", params: ["path": path])
            self.notice = "Added \(added.root) (\(added.summary))"
        }
    }

    public func removeWorkspace(_ root: String) async {
        await change {
            let _: Empty = try await self.caller.call("workspace.remove", params: ["root": root])
            self.notice = "Removed \(root)"
        }
    }

    private func change(_ body: @MainActor () async throws -> Void) async {
        if await run(body) { await refresh() }
    }

    @discardableResult
    private func run(_ body: @MainActor () async throws -> Void) async -> Bool {
        busy = true
        defer { busy = false }
        do {
            try await body()
            error = nil
            return true
        } catch AgentwsError.rpc(let rpc) {
            error = rpc.message
        } catch AgentwsError.disconnected {
            error = "The server is not connected"
        } catch {
            self.error = String(describing: error)
        }
        return false
    }
}
