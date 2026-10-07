#if canImport(SwiftUI)
import AgentwsKit
import AgentwsViews
import Foundation
import SwiftUI

enum Launch {
    static var binary: String {
        if let path = ProcessInfo.processInfo.environment["AGENTWS_BINARY"], !path.isEmpty { return path }
        if let bundled = Bundle.main.url(forAuxiliaryExecutable: "agentws-cli") { return bundled.path }
        return "agentws"
    }

    static var demo: Bool { CommandLine.arguments.contains("--demo") }

    @MainActor
    static func store() -> ViewStore {
        let binary = binary
        let build = (try? Build.read(binary: binary)) ?? "unknown"
        return ViewStore(endpoint: .local(binary: binary), build: build)
    }
}

struct AgentwsApp: App {
    var body: some Scene {
        WindowGroup("agentws") {
            if Launch.demo {
                MainWindow(scene: .seeded()).frame(minWidth: 900, minHeight: 560)
            } else {
                LiveWindow(store: Launch.store(), server: "This Mac")
            }
        }
    }
}

AgentwsApp.main()
#else
import AgentwsKit

print("agentws.app runs on macOS only (protocol v\(AgentwsProtocol.version))")
#endif
