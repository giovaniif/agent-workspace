#if canImport(SwiftUI)
import AgentwsKit
import AppKit
import ServiceManagement
import SwiftUI

public struct LiveSettings: View {
    private let store: SettingsStore
    private let server: ServerSettings?
    private let serverName: String
    private let cli: CLILink
    private let servers: ServerList?
    private let setup: ServerSetup?
    @Environment(\.openWindow) private var openWindow
    @State private var tab = SettingsTab.general
    @State private var refusal: String?
    @State private var localNotice: String?
    @State private var localError: String?
    @State private var cliStatus = CLILinkStatus.missing

    public init(
        store: SettingsStore, server: ServerSettings?, serverName: String, cli: CLILink,
        servers: ServerList? = nil, setup: ServerSetup? = nil
    ) {
        self.servers = servers
        self.setup = setup
        self.store = store
        self.server = server
        self.serverName = serverName
        self.cli = cli
    }

    public var body: some View {
        let scene = SettingsScene(
            tab: tab, server: serverName, settings: store.settings, agents: server?.agents, workspaces: server?.workspaces ?? [],
            cliLink: cliStatus, config: server?.config, harnesses: server?.harnesses ?? [], devices: server?.devices ?? [],
            pairing: server?.pairing, refusal: tab == .shortcuts ? refusal : nil,
            notice: localNotice ?? (tab == .servers ? setup?.notice : server?.notice),
            error: localError ?? (tab == .servers ? setup?.error : server?.error),
            servers: servers?.servers ?? [], selectedServer: servers?.selected ?? .thisMac,
            checklist: setup?.checklist ?? [], serverBusy: setup?.busy ?? false
        )
        SettingsView(scene: scene, actions: actions)
            .frame(width: 760, height: 600)
            .task { cliStatus = cli.status }
            .task(id: tab) {
                if tab.perServer { await server?.refresh() }
                if tab == .servers, setup?.busy != true { await setup?.check() }
            }
    }

    private var actions: SettingsActions {
        var a = SettingsActions()
        a.select = { value in
            tab = value
            refusal = nil
            localNotice = nil
            localError = nil
        }
        a.update = { settings in
            var settings = settings
            let previous = store.settings.general.openAtLogin
            if settings.general.openAtLogin != previous, !openAtLogin(settings.general.openAtLogin) {
                settings.general.openAtLogin = previous
            }
            store.settings = settings
        }
        a.assign = { assign($0, $1) }
        a.restoreShortcut = { assign($0, $0.defaultCombo) }
        a.restoreShortcuts = {
            store.settings.shortcuts.restoreDefaults()
            refusal = nil
        }
        a.linkCLI = { changeLink { try cli.install() } }
        a.unlinkCLI = { changeLink { try cli.remove() } }
        a.installHooks = { harness in act { await $0.installHooks(harness) } }
        a.removeHooks = { harness in act { await $0.removeHooks(harness) } }
        a.installNvim = { act { await $0.installNvim() } }
        a.addWorkspace = { path in act { await $0.addWorkspace(path) } }
        a.removeWorkspace = { root in act { await $0.removeWorkspace(root) } }
        a.refresh = { act { await $0.refresh() } }
        a.setConfig = { key, value in act { await $0.setConfig(key, to: value) } }
        a.revokeDevice = { id in act { await $0.revokeDevice(id) } }
        a.pairPhone = { act { await $0.pairPhone() } }
        a.selectServer = { servers?.selected = $0 }
        a.removeServer = { servers?.remove($0) }
        a.addServer = { openWindow(id: "setup") }
        a.checkServer = { serverTask { await $0.check() } }
        a.setUpServer = { serverTask { await $0.setUp() } }
        a.copy = { command in
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(command, forType: .string)
            localNotice = "Copied: \(command)"
        }
        return a
    }

    private func assign(_ action: ShortcutAction, _ combo: KeyCombo?) {
        var shortcuts = store.settings.shortcuts
        do throws(ShortcutRefusal) {
            try shortcuts.assign(combo, to: action)
            store.settings.shortcuts = shortcuts
            refusal = nil
        } catch {
            refusal = combo.map { error.message(for: $0) }
        }
    }

    private func act(_ body: @escaping @MainActor (ServerSettings) async -> Void) {
        guard let server else { return }
        localNotice = nil
        localError = nil
        Task { await body(server) }
    }

    private func serverTask(_ body: @escaping @MainActor (ServerSetup) async -> Void) {
        guard let setup else { return }
        localNotice = nil
        localError = nil
        Task { await body(setup) }
    }

    private func changeLink(_ body: () throws -> Void) {
        do {
            try body()
            localError = nil
        } catch let error as CLILinkError {
            localError = error.message
        } catch {
            localError = "\(error.localizedDescription) Run: sudo ln -sf '\(cli.target)' \(cli.path)"
        }
        cliStatus = cli.status
    }

    private func openAtLogin(_ on: Bool) -> Bool {
        do {
            if on {
                try SMAppService.mainApp.register()
            } else {
                try SMAppService.mainApp.unregister()
            }
            return true
        } catch {
            localError = "Open at login: \(error.localizedDescription)"
            return false
        }
    }
}
#endif
