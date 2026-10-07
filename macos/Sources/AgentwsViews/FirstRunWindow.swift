#if canImport(SwiftUI)
import AgentwsKit
import Foundation
import SwiftUI
import UserNotifications

public struct FirstRunView: View {
    private let appBuild: String
    private let localBinary: String
    private let bundled: @Sendable (Platform) -> String?
    private let hosts: [String]
    private let cli: CLILink
    private let finish: (ServerKind) -> Void

    @State private var flow = FirstRunFlow()
    @State private var typed = ""
    @State private var setup: ServerSetup?
    @State private var settings: ServerSettings?
    @State private var store: ViewStore?
    @State private var path = ""
    @State private var dirs: [String] = []
    @State private var linkNote: String?
    @State private var notifications: String?
    @Environment(\.colorScheme) private var scheme

    public init(
        appBuild: String, localBinary: String, bundled: @escaping @Sendable (Platform) -> String?,
        hosts: [String], cli: CLILink, finish: @escaping (ServerKind) -> Void
    ) {
        self.appBuild = appBuild
        self.localBinary = localBinary
        self.bundled = bundled
        self.hosts = hosts
        self.cli = cli
        self.finish = finish
    }

    public var body: some View {
        let theme = Theme(scheme)
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                ForEach(FirstRunStep.allCases, id: \.self) { step in
                    Text("\(step.rawValue + 1). \(step.title)")
                        .font(.system(size: 11, weight: step == flow.step ? .semibold : .regular))
                        .foregroundStyle(step == flow.step ? theme(.blue) : theme(.subtext))
                }
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme(.mantle))
            Divider()
            ScrollView {
                content
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(24)
            }
            Divider()
            HStack {
                if flow.step != .welcome {
                    Button("Back") { flow.back() }
                }
                Spacer()
                if flow.step == .workspace {
                    Button("Skip") { flow.next() }
                }
                if flow.step == .done {
                    Button("Open agentws") { finish(flow.kind) }.keyboardShortcut(.defaultAction)
                } else {
                    Button(flow.step == .welcome ? "Get started" : "Continue") { advance() }
                        .keyboardShortcut(.defaultAction)
                        .disabled(!canAdvance)
                }
            }
            .padding(16)
        }
        .frame(width: 640, height: 520)
        .background(theme(.base))
        .foregroundStyle(theme(.text))
    }

    @ViewBuilder
    private var content: some View {
        switch flow.step {
        case .welcome: welcome
        case .location: location
        case .host: host
        case .setup: serverSetup
        case .workspace: workspace
        case .done: done
        }
    }

    private var welcome: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Welcome to agentws").font(.system(size: 22, weight: .semibold))
            Text("Run Claude Code and Codex sessions in parallel, each in its own git worktrees, and review what they change.")
            Text("The next steps pick where agents run, set that machine up and add a workspace. Nothing needs sudo.")
                .foregroundStyle(.secondary)
        }
    }

    private var location: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Where do agents run?").font(.system(size: 18, weight: .semibold))
            choice("This Mac", "Sessions run here; the app starts the daemon as a launchd agent.", "laptopcomputer", selected: !flow.kind.isRemote) {
                flow.kind = .thisMac
            }
            choice("A server over SSH", "Sessions run on another machine; the app talks to it with your ssh, keys and ~/.ssh/config.", "server.rack", selected: flow.kind.isRemote) {
                if !flow.kind.isRemote { flow.kind = .ssh(host: hosts.first ?? "") }
            }
        }
    }

    private func choice(_ title: String, _ detail: String, _ symbol: String, selected: Bool, _ pick: @escaping () -> Void) -> some View {
        let theme = Theme(scheme)
        return Button(action: pick) {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: symbol).font(.system(size: 22)).frame(width: 30)
                VStack(alignment: .leading, spacing: 3) {
                    Text(title).font(.system(size: 14, weight: .semibold))
                    Text(detail).font(.system(size: 12)).foregroundStyle(.secondary)
                }
                Spacer()
            }
            .padding(12)
            .background(RoundedRectangle(cornerRadius: Metrics.corner).stroke(selected ? theme(.blue) : theme(.surface), lineWidth: selected ? 2 : 1))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private var host: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Which server?").font(.system(size: 18, weight: .semibold))
            Text("The app connects with ssh -o BatchMode=yes, so the server needs key or agent login (Tailscale SSH works too). It never asks for or stores a password.")
                .font(.system(size: 12)).foregroundStyle(.secondary)
            if !hosts.isEmpty {
                SettingsGroup(title: "From ~/.ssh/config") {
                    ForEach(hosts, id: \.self) { name in
                        Button { pick(name) } label: {
                            HStack {
                                Image(systemName: currentHost == name ? "largecircle.fill.circle" : "circle")
                                Text(name).font(Metrics.code)
                                Spacer()
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            HStack {
                TextField("user@host", text: $typed).font(Metrics.code).onSubmit(useTyped)
                Button("Use", action: useTyped).disabled(SSHConfig.target(typed) == nil)
            }
            if let setup, setup.server == flow.kind {
                if setup.busy {
                    ProgressView("Connecting to \(setup.server.name)…").controlSize(.small)
                } else if let error = setup.error {
                    Text(error).font(.system(size: 12)).foregroundStyle(.red)
                } else if let probe = setup.probe {
                    Text(summary(probe)).font(.system(size: 12))
                }
            }
        }
    }

    private var currentHost: String? {
        if case let .ssh(host) = flow.kind { return host }
        return nil
    }

    private func summary(_ probe: Probe) -> String {
        let installed = probe.build.map { $0 == appBuild ? "agentws \($0) is installed" : "agentws \($0) is installed; this app installs \(appBuild)" }
            ?? "agentws is not installed yet"
        return "\(probe.kernel) \(probe.machine), user \(probe.user). \(installed)."
    }

    private func useTyped() {
        guard let target = SSHConfig.target(typed) else { return }
        pick(target)
    }

    private func pick(_ host: String) {
        flow.kind = .ssh(host: host)
        let setup = makeSetup(flow.kind)
        Task { await setup.check() }
    }

    private func makeSetup(_ kind: ServerKind) -> ServerSetup {
        if let setup, setup.server == kind { return setup }
        let made = ServerSetup(server: kind, appBuild: appBuild, bundled: bundled, shell: ProcessShell(server: kind))
        setup = made
        settings = nil
        store?.stop()
        store = nil
        return made
    }

    private var serverSetup: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Set up \(flow.kind.name)").font(.system(size: 18, weight: .semibold))
            Text(flow.kind.isRemote
                 ? "Installs this app's agentws build into ~/.local/bin, runs the daemon as a systemd --user service (a launchd agent on a Mac) with your login shell's PATH, and checks git, tmux and gh. All in your account, no sudo."
                 : "Links the bundled agentws into /usr/local/bin, starts the daemon as a launchd agent and checks git, tmux and gh.")
                .font(.system(size: 12)).foregroundStyle(.secondary)
            if let setup, setup.server == flow.kind {
                ForEach(setup.checklist) { item in
                    CheckRow(item: item) { item in
                        if item.command == nil { run() }
                    }
                }
                if setup.busy {
                    ProgressView("Setting up…").controlSize(.small)
                }
                if let error = setup.error {
                    Text(error).font(.system(size: 12)).foregroundStyle(.red).textSelection(.enabled)
                }
                if let linger = setup.lingerCommand {
                    Text("Run this once on the server so sessions survive logout:").font(.system(size: 12))
                    Text(linger).font(Metrics.code).textSelection(.enabled)
                }
            }
            if let linkNote {
                Text(linkNote).font(.system(size: 12)).foregroundStyle(.secondary).textSelection(.enabled)
            }
            Button(ready ? "Run setup again" : "Set up", action: run).disabled(setup?.busy == true)
            if let settings, let agents = settings.agents {
                SettingsGroup(title: "Agent hooks (a backup is made first)") {
                    hooks("Claude Code", "claude", agents.harnesses["claude"], settings)
                    hooks("Codex (optional)", "codex", agents.harnesses["codex"], settings)
                }
            }
        }
    }

    private func hooks(_ title: String, _ key: String, _ harness: HarnessSetup?, _ settings: ServerSettings) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                Text(harness?.installed == true ? "Hooks installed in \(harness?.file ?? "")" : "Hooks not installed")
                    .font(.system(size: 11)).foregroundStyle(.secondary)
            }
            Spacer()
            if harness?.installed != true {
                Button("Install hooks") { Task { await settings.installHooks(key) } }
            }
        }
    }

    private var ready: Bool {
        guard let setup, setup.server == flow.kind, setup.error == nil, let probe = setup.probe else { return false }
        return probe.build == appBuild && probe.daemon?.running == true
    }

    private func run() {
        let setup = makeSetup(flow.kind)
        if !flow.kind.isRemote {
            do {
                try cli.install()
                linkNote = nil
            } catch let error as CLILinkError {
                linkNote = error.message
            } catch {
                linkNote = "\(error.localizedDescription). To link it yourself: sudo ln -sf '\(cli.target)' \(cli.path)"
            }
        }
        let kind = flow.kind
        Task {
            await setup.setUp()
            guard setup.error == nil, self.setup === setup, flow.kind == kind else { return }
            self.store?.stop()
            let store = ViewStore(endpoint: kind.endpoint(localBinary: localBinary), build: appBuild)
            let settings = ServerSettings(caller: store)
            self.store = store
            self.settings = settings
            await settings.refresh()
            guard self.settings === settings else { return }
            if path.isEmpty, let home = setup.probe?.home { path = home }
        }
    }

    private var workspace: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Add a workspace").font(.system(size: 18, weight: .semibold))
            Text("A folder on \(flow.kind.name): one repo, or a folder of repos (an orchestration root). Worktrees that already exist are adopted.")
                .font(.system(size: 12)).foregroundStyle(.secondary)
            HStack {
                TextField("Absolute path on the server", text: $path).font(Metrics.code)
                Button("Browse") { browse(path) }.disabled(path.isEmpty || settings == nil)
                Button("Add") { Task { await settings?.addWorkspace(path) } }.disabled(path.isEmpty || settings == nil)
            }
            if !dirs.isEmpty {
                SettingsGroup(title: "Folders in \(path)") {
                    ForEach(dirs, id: \.self) { dir in
                        Button {
                            path = dir
                            browse(dir)
                        } label: {
                            Label((dir as NSString).lastPathComponent, systemImage: "folder").frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            if let settings {
                if let error = settings.error {
                    Text(error).font(.system(size: 12)).foregroundStyle(.red)
                } else if let notice = settings.notice {
                    Text(notice).font(.system(size: 12)).foregroundStyle(.green)
                }
                ForEach(settings.workspaces) { workspace in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(workspace.root).font(Metrics.code)
                        Text(workspace.summary).font(.system(size: 11)).foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    private func browse(_ folder: String) {
        guard let settings else { return }
        Task { dirs = await settings.dirs(folder) }
    }

    private var done: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("All set").font(.system(size: 22, weight: .semibold))
            if let setup, let probe = setup.probe {
                Text("\(flow.kind.name): \(probe.kernel) \(probe.machine), agentws \(probe.build ?? appBuild), daemon \(probe.daemon?.running == true ? "running" : "not running").")
            }
            if let settings {
                Text("\(settings.workspaces.count) workspace\(settings.workspaces.count == 1 ? "" : "s") added.")
            }
            HStack {
                Button("Allow notifications") { requestNotifications() }
                if let notifications { Text(notifications).font(.system(size: 12)).foregroundStyle(.secondary) }
            }
            Text(flow.kind.isRemote
                 ? "The TUI shows the same sessions: ssh -t \(flow.kind.name) agentws"
                 : "The TUI shows the same sessions: run agentws in a terminal.")
                .font(Metrics.code).textSelection(.enabled)
        }
    }

    private func requestNotifications() {
        Task {
            let granted = (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])) ?? false
            notifications = granted ? "Allowed" : "Not allowed; change it in System Settings › Notifications"
        }
    }

    private var canAdvance: Bool {
        switch flow.step {
        case .welcome, .workspace, .done: true
        case .location: true
        case .host: currentHost.map { !$0.isEmpty } == true && setup?.server == flow.kind && setup?.probe != nil && setup?.error == nil && setup?.busy == false
        case .setup: ready
        }
    }

    private func advance() {
        flow.next()
        if flow.step == .setup, setup?.server != flow.kind {
            let setup = makeSetup(flow.kind)
            Task { await setup.check() }
        }
    }
}
#endif
