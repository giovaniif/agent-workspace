#if canImport(SwiftUI)
import AgentwsKit
import AgentwsViews
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
}

@MainActor
@Observable
final class Launcher {
    private(set) var store: ViewStore?
    private(set) var server: ServerSettings?
    let settings = SettingsStore()
    private var loading = false

    func load() async {
        guard store == nil, !loading else { return }
        loading = true
        let binary = Launch.binary
        let build = await Task.detached { (try? Build.read(binary: binary)) ?? "unknown" }.value
        let store = ViewStore(endpoint: .local(binary: binary), build: build)
        self.store = store
        server = ServerSettings(caller: store)
    }

    var colorScheme: ColorScheme? {
        switch settings.settings.appearance.theme {
        case .system: nil
        case .latte: .light
        case .mocha: .dark
        }
    }
}

struct AgentwsApp: App {
    @State private var launcher = Launcher()
    @State private var router = WindowRouter()

    var body: some Scene {
        WindowGroup("agentws", id: "main") {
            if Launch.demo {
                MainWindow(scene: .seeded()).frame(minWidth: 900, minHeight: 560)
            } else if let store = launcher.store {
                LiveWindow(store: store, server: "This Mac", router: router, shortcuts: launcher.settings.settings.shortcuts)
                    .preferredColorScheme(launcher.colorScheme)
            } else {
                MainWindow(scene: WindowScene(state: nil, connection: .connecting, now: .now))
                    .frame(minWidth: 900, minHeight: 560)
                    .task { await launcher.load() }
            }
        }
        .commands { CommandGroup(replacing: .newItem) {} }
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
        .keyboardShortcut("w", modifiers: [.command, .shift])
        Settings {
            LiveSettings(
                store: launcher.settings, server: launcher.server, serverName: "This Mac",
                cli: CLILink(target: Launch.binary)
            )
            .preferredColorScheme(launcher.colorScheme)
            .task { await launcher.load() }
        }
    }
}

AgentwsApp.main()
#else
import AgentwsKit

print("agentws.app runs on macOS only (protocol v\(AgentwsProtocol.version))")
#endif
