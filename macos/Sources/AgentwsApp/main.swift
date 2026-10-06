#if canImport(SwiftUI)
import SwiftUI

struct AgentwsApp: App {
    var body: some Scene {
        WindowGroup("agentws") {
            Color.clear.frame(minWidth: 900, minHeight: 600)
        }
    }
}

AgentwsApp.main()
#else
import AgentwsKit

print("agentws.app runs on macOS only (protocol v\(AgentwsProtocol.version))")
#endif
