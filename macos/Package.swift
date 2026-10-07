// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "agentws",
    platforms: [.macOS(.v15)],
    products: [
        .library(name: "AgentwsKit", targets: ["AgentwsKit"]),
        .executable(name: "AgentwsApp", targets: ["AgentwsApp"]),
    ],
    targets: [
        .target(name: "AgentwsKit"),
        .executableTarget(name: "AgentwsApp", dependencies: ["AgentwsKit"]),
        .testTarget(name: "AgentwsKitTests", dependencies: ["AgentwsKit"]),
    ]
)
