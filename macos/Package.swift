// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "agentws",
    platforms: [.macOS(.v15)],
    products: [
        .library(name: "AgentwsKit", targets: ["AgentwsKit"]),
        .library(name: "AgentwsViews", targets: ["AgentwsViews"]),
        .executable(name: "AgentwsApp", targets: ["AgentwsApp"]),
    ],
    targets: [
        .target(name: "AgentwsKit"),
        .target(name: "AgentwsViews", dependencies: ["AgentwsKit"]),
        .executableTarget(name: "AgentwsApp", dependencies: ["AgentwsKit", "AgentwsViews"]),
        .testTarget(name: "AgentwsKitTests", dependencies: ["AgentwsKit", "AgentwsViews"]),
    ]
)
