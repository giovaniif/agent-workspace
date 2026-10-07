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
    dependencies: [
        .package(url: "https://github.com/migueldeicaza/SwiftTerm", from: "1.20.0"),
    ],
    targets: [
        .target(name: "AgentwsKit"),
        .target(name: "AgentwsViews", dependencies: [
            "AgentwsKit",
            .product(name: "SwiftTerm", package: "SwiftTerm", condition: .when(platforms: [.macOS])),
        ]),
        .executableTarget(name: "AgentwsApp", dependencies: ["AgentwsKit", "AgentwsViews"]),
        .testTarget(name: "AgentwsKitTests", dependencies: ["AgentwsKit", "AgentwsViews"]),
    ]
)
