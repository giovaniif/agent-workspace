#if canImport(SwiftUI)
import AgentwsKit
import AgentwsViews
import AppKit
import Foundation
import SwiftUI

enum Launch {
    static var binary: String {
        Build.binary(
            environment: ProcessInfo.processInfo.environment,
            resources: Bundle.main.resourcePath,
            arch: arch,
            isExecutable: FileManager.default.isExecutableFile(atPath:)
        )
    }

    static var arch: String {
        #if arch(arm64)
        "arm64"
        #else
        "x86_64"
        #endif
    }

    static var demo: Bool { CommandLine.arguments.contains("--demo") }

    static var bundled: @Sendable (Platform) -> String? {
        let resources = Bundle.main.resourcePath
        let environment = ProcessInfo.processInfo.environment
        return { platform in
            if platform.os == "darwin", let path = environment["AGENTWS_BINARY"], !path.isEmpty { return path }
            guard let resources else { return nil }
            let path = "\(resources)/bin/\(platform.directory)/agentws"
            return FileManager.default.isExecutableFile(atPath: path) ? path : nil
        }
    }

    static var sshHosts: [String] {
        let config = (try? String(contentsOfFile: NSHomeDirectory() + "/.ssh/config", encoding: .utf8)) ?? ""
        return SSHConfig.hosts(config)
    }
}

@MainActor
@Observable
final class Launcher {
    private(set) var store: ViewStore?
    private(set) var server: ServerSettings?
    let settings = SettingsStore()
    private(set) var attention: Attention?
    private(set) var notifier: UserNotifier?
    private(set) var bridges: [BridgeAgent] = []
    private(set) var binary = ""
    let router = WindowRouter()
    var openMain: () -> Void = {}
    private var stream: NoticeStream?
    private var loading = false
    private(set) var setup: ServerSetup?
    private(set) var current: ServerKind?
    private(set) var build: String?
    let servers = ServerList()

    var needsFirstRun: Bool { servers.servers.isEmpty }

    func load() async {
        guard build == nil, !loading else { return }
        loading = true
        let binary = Launch.binary
        self.binary = binary
        build = await Task.detached { (try? Build.read(binary: binary)) ?? "unknown" }.value
        guard !needsFirstRun else { return }
        let named = settings.settings.general.launchServer
        connect(servers.servers.first { $0.name == named } ?? servers.selected)
    }

    func added(_ server: ServerKind) {
        servers.add(server)
        servers.selected = server
        guard current != server else { return }
        connect(server)
    }

    func connect(_ server: ServerKind) {
        guard let build, current != server else { return }
        store?.stop()
        stream?.stop()
        if servers.selected != server { servers.selected = server }
        let store = ViewStore(endpoint: server.endpoint(localBinary: binary), build: build)
        setup = ServerSetup(server: server, appBuild: build, bundled: Launch.bundled, shell: ProcessShell(server: server))
        current = server
        let freshNotifier = self.notifier == nil
        let notifier = self.notifier ?? UserNotifier { [weak self] action in self?.handle(action) }
        let attention = Attention(caller: store, notifier: notifier)
        attention.preferences = settings.settings.notifications
        attention.isMuted = { [weak store] id in store?.state?.sessions.first { $0.id == id }?.muted ?? false }
        store.callsRestarted = { [weak attention] in
            if let attention { Task { await attention.reconnected() } }
        }
        let stream = NoticeStream(endpoint: store.endpoint, build: build, connected: { [weak attention] in
            if let attention { Task { await attention.reconnected() } }
        }) { [weak attention] notice in
            attention?.receive(notice)
        }
        self.store = store
        self.server = ServerSettings(caller: store)
        self.notifier = notifier
        self.attention = attention
        self.stream = stream
        store.start()
        if freshNotifier { notifier.start() }
        stream.start()
        findBridges()
    }

    func handle(_ action: BannerAction) {
        guard let attention else { return }
        switch action {
        case let .allow(id): Task { await attention.allow(id) }
        case let .reply(id, text): Task { await attention.reply(id, text: text) }
        case let .open(id): open(id)
        }
    }

    func open(_ id: String) {
        router.goTo(session: id)
        openMain()
        NSApplication.shared.activate()
    }

    private var launchAgents: URL {
        FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/LaunchAgents")
    }

    func findBridges() {
        guard case let .ssh(host, _)? = store?.endpoint else {
            bridges = []
            return
        }
        bridges = BridgeAgents.find(in: launchAgents).filter { $0.host == host }
    }

    func removeBridges() {
        let argvs = bridges.map { $0.removeArgv(binary: binary) }
        Task {
            await Task.detached {
                for argv in argvs {
                    let process = Process()
                    process.executableURL = URL(fileURLWithPath: argv[0])
                    process.arguments = Array(argv.dropFirst())
                    try? process.run()
                    process.waitUntilExit()
                }
            }.value
            findBridges()
        }
    }

    func updateBadge() {
        guard let store else { return }
        NSApplication.shared.dockTile.badgeLabel = AttentionMenu(state: store.state, now: .now).badge
    }

    var colorScheme: ColorScheme? {
        switch settings.settings.appearance.theme {
        case .system: nil
        case .latte: .light
        case .mocha: .dark
        }
    }
}

struct MenuContent: View {
    let launcher: Launcher
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        let scene = MenuScene(
            menu: AttentionMenu(state: launcher.store?.state, now: .now),
            connection: launcher.store?.connection ?? .connecting,
            notifications: launcher.notifier?.problem,
            bridges: launcher.bridges,
            message: launcher.attention?.message
        )
        AttentionMenuView(scene: scene, actions: actions)
            .task { await launcher.load() }
            .onAppear {
                launcher.openMain = { openWindow(id: "main") }
                launcher.findBridges()
            }
    }

    private var actions: MenuActions {
        var a = MenuActions()
        a.allow = { id in if let attention = launcher.attention { Task { await attention.allow(id) } } }
        a.always = { id in if let attention = launcher.attention { Task { await attention.allow(id, always: true) } } }
        a.open = { launcher.open($0) }
        a.newSession = {
            launcher.router.newSession()
            openWindow(id: "main")
            NSApplication.shared.activate()
        }
        a.openApp = {
            openWindow(id: "main")
            NSApplication.shared.activate()
        }
        a.removeBridges = { launcher.removeBridges() }
        a.dismissMessage = { launcher.attention?.message = nil }
        a.quit = { NSApplication.shared.terminate(nil) }
        return a
    }
}

struct MenuLabel: View {
    let launcher: Launcher
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        let menu = AttentionMenu(state: launcher.store?.state, now: .now)
        HStack(spacing: 3) {
            Image(systemName: menu.needsYou.isEmpty ? "square.stack.3d.up" : "square.stack.3d.up.badge.automatic")
            if !menu.title.isEmpty { Text(menu.title) }
        }
        .task {
            launcher.openMain = { openWindow(id: "main") }
            if !Launch.demo { await launcher.load() }
        }
        .onChange(of: menu.badge, initial: true) { launcher.updateBadge() }
        .onChange(of: launcher.settings.settings.notifications) {
            launcher.attention?.preferences = launcher.settings.settings.notifications
        }
    }
}

struct FirstRunHost: View {
    let launcher: Launcher
    let build: String
    @Environment(\.dismissWindow) private var dismissWindow

    var body: some View {
        FirstRunView(
            appBuild: build, localBinary: Launch.binary, bundled: Launch.bundled, hosts: Launch.sshHosts,
            cli: CLILink(target: Launch.binary),
            cancel: { launcher.needsFirstRun ? NSApplication.shared.terminate(nil) : dismissWindow(id: "setup") }
        ) { server in
            launcher.added(server)
            dismissWindow(id: "setup")
        }
    }
}

struct AgentwsApp: App {
    @State private var launcher = Launcher()
    private var router: WindowRouter { launcher.router }

    var body: some Scene {
        WindowGroup("agentws", id: "main") {
            if Launch.demo {
                MainWindow(scene: .seeded()).frame(minWidth: 900, minHeight: 560)
            } else if let build = launcher.build, launcher.needsFirstRun {
                FirstRunHost(launcher: launcher, build: build).preferredColorScheme(launcher.colorScheme)
            } else if let store = launcher.store, let current = launcher.current {
                LiveWindow(
                    store: store, server: current.name, router: router, shortcuts: launcher.settings.settings.shortcuts,
                    attention: launcher.attention, diffLayout: launcher.settings.settings.appearance.diffLayout,
                    confirmEnd: launcher.settings.settings.general.confirmOnEnd, servers: launcher.servers.servers, switchServer: { launcher.connect($0) }
                )
                .id(current)
                .preferredColorScheme(launcher.colorScheme)
            } else {
                MainWindow(scene: WindowScene(state: nil, connection: .connecting, now: .now))
                    .frame(minWidth: 900, minHeight: 560)
                    .task { await launcher.load() }
            }
        }
        .commands { CommandGroup(replacing: .newItem) {} }
        Window("Add a server", id: "setup") {
            if let build = launcher.build {
                FirstRunHost(launcher: launcher, build: build).preferredColorScheme(launcher.colorScheme)
            } else {
                ProgressView().frame(minWidth: 760, idealWidth: 860, minHeight: 520, idealHeight: 600).task { await launcher.load() }
            }
        }
        .windowResizability(.contentSize)
        Window("Worktrees and disk", id: "disk") {
            if Launch.demo {
                DiskWindow(scene: .seeded()).frame(minWidth: 820, minHeight: 480)
            } else if let store = launcher.store {
                LiveDiskWindow(store: store, router: router)
            } else {
                DiskWindow(scene: DiskScene(disk: nil, state: nil, now: .now))
                    .frame(minWidth: 820, minHeight: 480)
                    .task { await launcher.load() }
            }
        }
        .keyboardShortcut(launcher.settings.settings.shortcuts.combo(for: .worktrees).map(\.keyboardShortcut))
        Settings {
            LiveSettings(
                store: launcher.settings, server: launcher.server, serverName: launcher.current?.name ?? "This Mac",
                cli: CLILink(target: Launch.binary), servers: launcher.servers, setup: launcher.setup
            )
            .onChange(of: launcher.servers.selected) { launcher.connect(launcher.servers.selected) }
            .preferredColorScheme(launcher.colorScheme)
            .task { await launcher.load() }
        }
        MenuBarExtra {
            if Launch.demo {
                AttentionMenuView(scene: .seeded())
            } else {
                MenuContent(launcher: launcher)
            }
        } label: {
            MenuLabel(launcher: launcher)
        }
        .menuBarExtraStyle(.window)
    }
}

AgentwsApp.main()
#else
import AgentwsKit

print("agentws.app runs on macOS only (protocol v\(AgentwsProtocol.version))")
#endif
