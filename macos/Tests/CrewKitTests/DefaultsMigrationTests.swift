import Foundation
import Testing
@testable import CrewKit

struct DefaultsMigrationTests {
    func fresh() -> (UserDefaults, String) {
        let suite = "crew-defaults-\(UUID().uuidString)"
        return (UserDefaults(suiteName: suite)!, suite)
    }

    @Test func theOldDomainIsCopiedOnceAndNewerValuesWin() {
        let (defaults, suite) = fresh()
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set("mine", forKey: "servers.selected")
        let old: [String: Any] = ["servers": Data("[1]".utf8), "servers.selected": "box", "settings.general": Data("{}".utf8)]
        #expect(DefaultsMigration.run(old: old, into: defaults))
        #expect(defaults.data(forKey: "servers") == Data("[1]".utf8))
        #expect(defaults.data(forKey: "settings.general") == Data("{}".utf8))
        #expect(defaults.string(forKey: "servers.selected") == "mine")
        #expect(!DefaultsMigration.run(old: ["servers": Data("[2]".utf8)], into: defaults))
        #expect(defaults.data(forKey: "servers") == Data("[1]".utf8))
    }

    @Test func noOldDomainOnlyMarksTheMigrationDone() {
        let (defaults, suite) = fresh()
        defer { defaults.removePersistentDomain(forName: suite) }
        #expect(!DefaultsMigration.run(old: [:], into: defaults))
        #expect(defaults.bool(forKey: DefaultsMigration.doneKey))
    }

    @Test func theOldDomainIsTheAgentwsBundleID() {
        #expect(DefaultsMigration.legacyDomain == "dev.agentws.app")
    }
}
