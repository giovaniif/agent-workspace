// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "crew",
    platforms: [.macOS(.v15)],
    products: [
        .library(name: "CrewKit", targets: ["CrewKit"]),
        .library(name: "CrewViews", targets: ["CrewViews"]),
        .executable(name: "CrewApp", targets: ["CrewApp"]),
    ],
    dependencies: [
        .package(url: "https://github.com/migueldeicaza/SwiftTerm", from: "1.20.0"),
    ],
    targets: [
        .target(name: "CrewKit"),
        .target(name: "CrewViews", dependencies: [
            "CrewKit",
            .product(name: "SwiftTerm", package: "SwiftTerm", condition: .when(platforms: [.macOS])),
        ]),
        .executableTarget(name: "CrewApp", dependencies: ["CrewKit", "CrewViews"]),
        .testTarget(name: "CrewKitTests", dependencies: ["CrewKit", "CrewViews"]),
    ]
)
