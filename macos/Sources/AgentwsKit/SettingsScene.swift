import Foundation

public struct SettingsScene: Sendable, Equatable {
    public var tab: SettingsTab
    public var server: String
    public var settings: AppSettings
    public var agents: AgentsStatus?
    public var workspaces: [WorkspaceInfo]
    public var cliLink: CLILinkStatus
    public var refusal: String?
    public var notice: String?
    public var error: String?

    public init(
        tab: SettingsTab = .general, server: String = "This Mac", settings: AppSettings = AppSettings(),
        agents: AgentsStatus? = nil, workspaces: [WorkspaceInfo] = [], cliLink: CLILinkStatus = .missing,
        refusal: String? = nil, notice: String? = nil, error: String? = nil
    ) {
        self.tab = tab
        self.server = server
        self.settings = settings
        self.agents = agents
        self.workspaces = workspaces
        self.cliLink = cliLink
        self.refusal = refusal
        self.notice = notice
        self.error = error
    }

    public static func seeded(tab: SettingsTab = .general) -> SettingsScene {
        SettingsScene(
            tab: tab,
            agents: AgentsStatus(
                done: true,
                harnesses: [
                    "claude": HarnessSetup(installed: true, file: "~/.claude/settings.json", backup: "~/.claude/settings.json.agentws.bak"),
                    "codex": HarnessSetup(installed: false, file: "~/.codex/hooks.json"),
                ],
                nvim: NvimSetup(onPath: true, configured: true, configFile: "~/.config/nvim/plugin/agentws.lua", pluginDir: "~/.local/share/agentws/nvim", pluginFound: true)
            ),
            workspaces: [
                WorkspaceInfo(root: "~/code/platform", kind: "orchestration", repos: [
                    RepoInfo(name: "api", path: "~/code/platform/api", branch: "main"),
                    RepoInfo(name: "web", path: "~/code/platform/web", branch: "42-retry"),
                    RepoInfo(name: "worker", path: "~/code/platform/worker", branch: "main"),
                ]),
                WorkspaceInfo(root: "~/code/notes", kind: "single", repos: [RepoInfo(name: "notes", path: "~/code/notes", branch: "main")]),
            ],
            cliLink: .linked
        )
    }
}
