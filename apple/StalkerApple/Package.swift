// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "StalkerApple",
    defaultLocalization: "en",
    platforms: [
        .macOS(.v14),
        .iOS(.v17),
        .watchOS(.v10),
    ],
    products: [
        .executable(name: "StalkerMac", targets: ["StalkerMac"]),
        .library(name: "StalkerShared", targets: ["StalkerShared"]),
        .library(name: "StalkerPhone", targets: ["StalkerPhone"]),
        .library(name: "StalkerWatch", targets: ["StalkerWatch"]),
        .library(name: "StalkerWidgets", targets: ["StalkerWidgets"]),
    ],
    targets: [
        .target(name: "StalkerShared"),
        .executableTarget(name: "StalkerMac", dependencies: ["StalkerShared"]),
        .target(name: "StalkerPhone", dependencies: ["StalkerShared"]),
        .target(name: "StalkerWatch", dependencies: ["StalkerShared"]),
        .target(name: "StalkerWidgets", dependencies: ["StalkerShared"]),
        .testTarget(name: "StalkerSharedTests", dependencies: ["StalkerShared"]),
    ]
)
