import Foundation

public struct SettingsScene: Sendable, Equatable {
    public var tab: SettingsTab
    public var server: String
    public var settings: AppSettings
    public var agents: AgentsStatus?
    public var workspaces: [WorkspaceInfo]
    public var cliLink: CLILinkStatus
    public var config: ServerConfig?
    public var harnesses: [HarnessOptions]
    public var devices: [PairedDevice]
    public var pairing: PairingCode?
    public var refusal: String?
    public var notice: String?
    public var error: String?

    public init(
        tab: SettingsTab = .general, server: String = "This Mac", settings: AppSettings = AppSettings(),
        agents: AgentsStatus? = nil, workspaces: [WorkspaceInfo] = [], cliLink: CLILinkStatus = .missing,
        config: ServerConfig? = nil, harnesses: [HarnessOptions] = [], devices: [PairedDevice] = [], pairing: PairingCode? = nil,
        refusal: String? = nil, notice: String? = nil, error: String? = nil
    ) {
        self.tab = tab
        self.server = server
        self.settings = settings
        self.agents = agents
        self.workspaces = workspaces
        self.cliLink = cliLink
        self.config = config
        self.harnesses = harnesses
        self.devices = devices
        self.pairing = pairing
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
            cliLink: .linked,
            config: ServerConfig(path: "~/.agentws/config.toml", values: [
                "launcher.max_parallel": "4", "fallback.threshold": "20", "push.away_after": "2m",
                "defaults.claude.model": "opus", "defaults.claude.effort": "high", "theme.blue": "#1e66f5",
            ]),
            harnesses: [
                HarnessOptions(harness: "claude", name: "Claude Code", tag: "CC", models: ["opus", "sonnet", "haiku"], efforts: ["low", "medium", "high", "xhigh", "max"], model: "opus", effort: "high"),
                HarnessOptions(harness: "codex", name: "Codex", tag: "CX", models: ["gpt-6-sol", "gpt-6-luna"], efforts: ["low", "medium", "high"], model: "", effort: ""),
            ],
            devices: [PairedDevice(id: "d1", name: "iPhone", createdAt: "2026-10-01T10:00:00Z", lastSeen: "2026-10-06T09:30:00Z")]
        )
    }
}
