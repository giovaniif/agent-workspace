import Foundation

public struct ServerConfig: Codable, Equatable, Sendable {
    public var path: String
    public var values: [String: String]

    public init(path: String, values: [String: String]) {
        self.path = path
        self.values = values
    }

    public func value(_ key: String) -> String { values[key] ?? "" }

    public var worktreeLocation: String {
        let home = (path as NSString).deletingLastPathComponent
        return (home as NSString).appendingPathComponent("worktrees")
    }
}

public struct ConfigField: Equatable, Sendable, Identifiable {
    public var key: String
    public var title: String
    public var placeholder: String
    public var choices: [String]

    public init(key: String, title: String, placeholder: String, choices: [String] = []) {
        self.key = key
        self.title = title
        self.placeholder = placeholder
        self.choices = choices
    }

    public var id: String { key }
}

public enum ConfigFields {
    public static let workspaces = [
        ConfigField(key: "launcher.max_parallel", title: "Launcher: sessions started at once", placeholder: "3"),
    ]

    public static let agents = [
        ConfigField(key: "fallback.threshold", title: "Offer Codex when Claude's quota is under (%)", placeholder: "20"),
    ]

    public static let notifications = [
        ConfigField(key: "push.away_after", title: "Hold phone pushes while I typed in the last", placeholder: "2m"),
    ]

    public static let themeKeys = [
        "text", "subtext", "overlay", "surface", "mantle", "base", "blue", "peach", "green", "red", "teal", "mauve", "selected", "added_bg", "deleted_bg",
    ]

    public static let theme = themeKeys.map {
        ConfigField(key: "theme.\($0)", title: $0.replacingOccurrences(of: "_", with: " "), placeholder: "#rrggbb")
    }

    public static func defaults(for harness: String, models: [String] = [], efforts: [String] = []) -> [ConfigField] {
        [
            ConfigField(key: "defaults.\(harness).model", title: "Default model", placeholder: "harness default", choices: models),
            ConfigField(key: "defaults.\(harness).effort", title: "Default effort", placeholder: "harness default", choices: efforts),
        ]
    }
}

public struct PairedDevice: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var createdAt: String
    public var lastSeen: String

    enum CodingKeys: String, CodingKey {
        case id, name, createdAt = "created_at", lastSeen = "last_seen"
    }

    public init(id: String, name: String, createdAt: String, lastSeen: String) {
        self.id = id
        self.name = name
        self.createdAt = createdAt
        self.lastSeen = lastSeen
    }
}

public struct PairingCode: Codable, Equatable, Sendable {
    public var code: String
    public var expiresAt: String

    enum CodingKeys: String, CodingKey {
        case code, expiresAt = "expires_at"
    }

    public init(code: String, expiresAt: String) {
        self.code = code
        self.expiresAt = expiresAt
    }
}

struct DeviceListReply: Decodable, Sendable {
    @NullAsEmpty var devices: [PairedDevice]
}

struct ConfigSetParams: Encodable, Sendable {
    var key: String
    var value: String
}
