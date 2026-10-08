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

    private let cancel: (() -> Void)?

    @State private var flow: FirstRunFlow
    @State private var typed = ""
    @State private var setup: ServerSetup?
    @State private var settings: ServerSettings?
    @State private var store: ViewStore?
    @State private var linkNote: String?
    @State private var notifications: String?

    public init(
        appBuild: String, localBinary: String, bundled: @escaping @Sendable (Platform) -> String?,
        hosts: [String], cli: CLILink, initial: FirstRunFlow = FirstRunFlow(), cancel: (() -> Void)? = nil,
        finish: @escaping (ServerKind) -> Void
    ) {
        self._flow = State(initialValue: initial)
        self.cancel = cancel
        self.appBuild = appBuild
        self.localBinary = localBinary
        self.bundled = bundled
        self.hosts = hosts
        self.cli = cli
        self.finish = finish
    }

    public var body: some View {
        HStack(spacing: 0) {
            stepList
            Divider()
            VStack(spacing: 0) {
                ScrollView {
                    content
                        .frame(maxWidth: 620, alignment: .leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 32)
                        .padding(.vertical, 28)
                }
                Divider()
                buttonBar
            }
        }
        .frame(minWidth: 760, idealWidth: 860, maxWidth: .infinity, minHeight: 520, idealHeight: 600, maxHeight: .infinity)
        .background(Color(nsColor: .windowBackgroundColor))
    }

    private var stepList: some View {
        List(flow.steps, id: \.self, selection: .constant(Optional(flow.step))) { step in
            Label {
                Text(step.title)
            } icon: {
                Image(systemName: flow.isComplete(step) ? "checkmark.circle.fill" : "\(flow.steps.firstIndex(of: step).map { $0 + 1 } ?? 0).circle")
                    .foregroundStyle(flow.isComplete(step) ? Color.green : Color.secondary)
            }
            .foregroundStyle(step == flow.step || flow.isComplete(step) ? .primary : .secondary)
            .tag(step)
        }
        .listStyle(.sidebar)
        .scrollDisabled(true)
        .allowsHitTesting(false)
        .frame(width: 210)
    }

    private var buttonBar: some View {
        HStack {
            if let cancel {
                Button("Cancel", action: cancel)
                    .keyboardShortcut(.cancelAction)
                    .background { Button("", action: cancel).keyboardShortcut(".", modifiers: .command).opacity(0).accessibilityHidden(true) }
            }
            Spacer()
            if flow.canGoBack {
                Button("Back") { flow.back() }
            }
            if flow.step == .workspace {
                Button("Skip") { flow.next() }
            }
            Button(flow.primaryTitle) {
                if flow.step == .done { finish(flow.kind) } else { advance() }
            }
            .keyboardShortcut(.defaultAction)
            .disabled(!canAdvance)
        }
        .controlSize(.large)
        .padding(.horizontal, 20)
        .padding(.vertical, 14)
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
            Text("Welcome to agentws").font(.largeTitle.weight(.semibold))
            Text("Run Claude Code and Codex sessions in parallel, each in its own git worktrees, and review what they change.")
            Text("The next steps pick where agents run, set that machine up and add a workspace. Nothing needs sudo.")
                .foregroundStyle(.secondary)
        }
    }

    private var location: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Where do agents run?").font(.title2.weight(.semibold))
            Picker("Where do agents run?", selection: remote) {
                option("This Mac", "Sessions run here; the app starts the daemon as a launchd agent.").tag(false)
                option("A server over SSH", "Sessions run on another machine; the app talks to it with your ssh, keys and ~/.ssh/config.").tag(true)
            }
            .pickerStyle(.radioGroup)
            .labelsHidden()
        }
    }

    private var remote: Binding<Bool> {
        Binding(
            get: { flow.kind.isRemote },
            set: { isRemote in
                guard isRemote != flow.kind.isRemote else { return }
                if !isRemote {
                    flow.kind = .thisMac
                } else if let first = hosts.first {
                    pick(first)
                } else {
                    flow.kind = .ssh(host: "")
                }
            }
        )
    }

    private func option(_ title: String, _ detail: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
            Text(detail).font(.callout).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
        }
        .padding(.bottom, 6)
    }

    private var host: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Which server?").font(.title2.weight(.semibold))
            Text("The app connects with ssh -o BatchMode=yes, so the server needs key or agent login (Tailscale SSH works too). It never asks for or stores a password.")
                .font(.callout).foregroundStyle(.secondary)
            if !hosts.isEmpty {
                SettingsGroup(title: "From ~/.ssh/config") {
                    Picker("Host", selection: hostChoice) {
                        ForEach(hosts, id: \.self) { name in
                            Text(name).tag(Optional(name))
                        }
                    }
                    .pickerStyle(.radioGroup)
                    .labelsHidden()
                }
            }
            HStack {
                TextField("user@host", text: $typed).textFieldStyle(.roundedBorder).onSubmit(useTyped)
                Button("Use", action: useTyped).disabled(SSHConfig.target(typed) == nil)
            }
            if let setup, setup.server == flow.kind {
                if setup.busy {
                    ProgressView("Connecting to \(setup.server.name)…").controlSize(.small)
                } else if let error = setup.error {
                    Text(error).font(.callout).foregroundStyle(.red)
                } else if let probe = setup.probe {
                    Text(summary(probe)).font(.callout)
                }
            }
        }
    }

    private var hostChoice: Binding<String?> {
        Binding(get: { currentHost }, set: { name in if let name { pick(name) } })
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
            Text("Set up \(flow.kind.name)").font(.title2.weight(.semibold))
            Text(flow.kind.isRemote
                 ? "Installs this app's agentws build into ~/.local/bin, runs the daemon as a systemd --user service (a launchd agent on a Mac) with your login shell's PATH, and checks git, tmux and gh. All in your account, no sudo."
                 : "Links the bundled agentws into /usr/local/bin, starts the daemon as a launchd agent and checks git, tmux and gh.")
                .font(.callout).foregroundStyle(.secondary)
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
                    Text(error).font(.callout).foregroundStyle(.red).textSelection(.enabled)
                }
                if let linger = setup.lingerCommand {
                    Text("Run this once on the server so sessions survive logout:").font(.callout)
                    Text(linger).font(Metrics.code).textSelection(.enabled)
                }
            }
            if let linkNote {
                Text(linkNote).font(.callout).foregroundStyle(.secondary).textSelection(.enabled)
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
                    .font(.caption).foregroundStyle(.secondary)
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
            if settings.path.home.isEmpty, let home = setup.probe?.home { settings.path.home = home }
            if settings.path.text.isEmpty { settings.path.text = "~/" }
        }
    }

    private var workspace: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Add a workspace").font(.title2.weight(.semibold))
            Text("A folder on \(flow.kind.name): one repo, or a folder of repos (an orchestration root). Worktrees that already exist are adopted.")
                .font(.callout).foregroundStyle(.secondary)
            if let settings {
                WorkspacePathField(input: settings.path, busy: settings.busy) {
                    Task { await settings.addWorkspace(settings.path.resolved) }
                }
            } else {
                WorkspacePathField(input: WorkspacePathInput(), enabled: false) {}
            }
            if let settings {
                if let error = settings.error {
                    Text(error).font(.callout).foregroundStyle(.red)
                } else if let notice = settings.notice {
                    Text(notice).font(.callout).foregroundStyle(.green)
                }
                ForEach(settings.workspaces) { workspace in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(workspace.root).font(Metrics.code)
                        Text(workspace.summary).font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    private var done: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("All set").font(.largeTitle.weight(.semibold))
            if let setup, let probe = setup.probe {
                Text("\(flow.kind.name): \(probe.kernel) \(probe.machine), agentws \(probe.build ?? appBuild), daemon \(probe.daemon?.running == true ? "running" : "not running").")
            }
            if let settings {
                Text("\(settings.workspaces.count) workspace\(settings.workspaces.count == 1 ? "" : "s") added.")
            }
            HStack {
                Button("Allow notifications") { requestNotifications() }
                if let notifications { Text(notifications).font(.callout).foregroundStyle(.secondary) }
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
