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
    public var servers: [ServerKind] = []
    public var selectedServer: ServerKind = .thisMac
    public var checklist: [CheckItem] = []
    public var serverBusy = false

    public init(
        tab: SettingsTab = .general, server: String = "This Mac", settings: AppSettings = AppSettings(),
        agents: AgentsStatus? = nil, workspaces: [WorkspaceInfo] = [], cliLink: CLILinkStatus = .missing,
        config: ServerConfig? = nil, harnesses: [HarnessOptions] = [], devices: [PairedDevice] = [], pairing: PairingCode? = nil,
        refusal: String? = nil, notice: String? = nil, error: String? = nil,
        servers: [ServerKind] = [], selectedServer: ServerKind = .thisMac, checklist: [CheckItem] = [], serverBusy: Bool = false
    ) {
        self.servers = servers
        self.selectedServer = selectedServer
        self.checklist = checklist
        self.serverBusy = serverBusy
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
            devices: [PairedDevice(id: "d1", name: "iPhone", createdAt: "2026-10-01T10:00:00Z", lastSeen: "2026-10-06T09:30:00Z")],
            servers: [.thisMac, .ssh(host: "devbox")],
            selectedServer: .ssh(host: "devbox"),
            checklist: [
                CheckItem(id: "ssh", title: "SSH", state: .ok, detail: "devbox reachable · 38 ms · Linux x86_64"),
                CheckItem(id: "agentws", title: "agentws", state: .warning, detail: "build v0.9.0 1a2b3c, the app is v1.0.0 4d5e6f", fix: "Update server"),
                CheckItem(id: "daemon", title: "Daemon", state: .warning, detail: "running, but linger is off: the daemon and every session stop at logout", command: "sudo loginctl enable-linger me"),
                CheckItem(id: "git", title: "git", state: .ok, detail: "found"),
                CheckItem(id: "tmux", title: "tmux", state: .ok, detail: "found"),
                CheckItem(id: "gh", title: "gh", state: .warning, detail: "not signed in", fix: "Run gh auth login", command: "ssh -t devbox gh auth login"),
            ]
        )
    }
}
