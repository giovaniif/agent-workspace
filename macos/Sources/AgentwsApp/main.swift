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
    private var loading = false

    func load() async {
        guard store == nil, !loading else { return }
        loading = true
        let binary = Launch.binary
        let build = await Task.detached { (try? Build.read(binary: binary)) ?? "unknown" }.value
        store = ViewStore(endpoint: .local(binary: binary), build: build)
    }
}

struct AgentwsApp: App {
    @State private var launcher = Launcher()

    var body: some Scene {
        WindowGroup("agentws") {
            if Launch.demo {
                MainWindow(scene: .seeded()).frame(minWidth: 900, minHeight: 560)
            } else if let store = launcher.store {
                LiveWindow(store: store, server: "This Mac")
            } else {
                MainWindow(scene: WindowScene(state: nil, connection: .connecting, now: .now))
                    .frame(minWidth: 900, minHeight: 560)
                    .task { await launcher.load() }
            }
        }
    }
}

AgentwsApp.main()
#else
import AgentwsKit

print("agentws.app runs on macOS only (protocol v\(AgentwsProtocol.version))")
#endif
