import Foundation

public enum DefaultsMigration {
    public static let legacyDomain = "dev.agentws.app"
    public static let doneKey = "migration.agentws"

    @discardableResult
    public static func run(old: [String: Any], into defaults: UserDefaults) -> Bool {
        guard !defaults.bool(forKey: doneKey) else { return false }
        var copied = false
        for (key, value) in old where defaults.object(forKey: key) == nil {
            defaults.set(value, forKey: key)
            copied = true
        }
        defaults.set(true, forKey: doneKey)
        return copied
    }
}
