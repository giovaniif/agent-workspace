import Foundation
import Testing
@testable import AgentwsKit

enum Goldens {
    static let dir = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .appendingPathComponent("internal/view/testdata")

    static func data(_ name: String) throws -> Data {
        try Data(contentsOf: dir.appendingPathComponent(name))
    }

    static func state() throws -> ViewState {
        try JSONDecoder().decode(ViewState.self, from: data("view-subscribe-state.json"))
    }

    static func diffs() throws -> [ViewDiff] {
        try JSONDecoder().decode([ViewDiff].self, from: data("view-subscribe-diffs.json"))
    }
}

func missingKeys(golden: Any, encoded: Any, path: String = "$") -> [String] {
    switch (golden, encoded) {
    case let (g as [String: Any], e as [String: Any]):
        return g.keys.sorted().flatMap { key -> [String] in
            guard let gv = g[key], !(gv is NSNull) else { return [] }
            guard let ev = e[key] else { return ["\(path).\(key)"] }
            return missingKeys(golden: gv, encoded: ev, path: "\(path).\(key)")
        }
    case let (g as [Any], e as [Any]):
        guard g.count == e.count else { return ["\(path) has \(e.count) items, want \(g.count)"] }
        return zip(g, e).enumerated().flatMap { i, pair in
            missingKeys(golden: pair.0, encoded: pair.1, path: "\(path)[\(i)]")
        }
    default:
        return []
    }
}

func roundTripMissingKeys<T: Codable>(_ type: T.Type, golden name: String) throws -> [String] {
    let raw = try Goldens.data(name)
    let decoded = try JSONDecoder().decode(type, from: raw)
    let encoded = try JSONEncoder().encode(decoded)
    return missingKeys(
        golden: try JSONSerialization.jsonObject(with: raw),
        encoded: try JSONSerialization.jsonObject(with: encoded)
    )
}

struct ViewDecodingTests {
    @Test func decodesTheGoGoldenState() throws {
        let state = try Goldens.state()
        #expect(state.seq == 10)
        #expect(state.workspaces.map(\.root) == ["/w/api"])
        #expect(state.tasks.map(\.text) == ["add login", "fix retry"])
        #expect(state.worktrees.first?.ports?.first?.port == 3000)
        #expect(state.sessions.map(\.id) == ["s1", "s2", "s3"])
        #expect(state.sessions.map(\.order) == [1, 2, 0])
        #expect(state.sessions[2].banner == "needs permission")
        #expect(state.sessions[2].state == "permission")
        #expect(state.events.first?.tool == "Bash")
        #expect(state.subagents.first?.type == "Explore")
    }

    @Test func decodesTheGoGoldenDiffs() throws {
        let diffs = try Goldens.diffs()
        #expect(diffs.count == 10)
        #expect(diffs[0].worktree?.pr?.failing.first?.name == "test")
        #expect(diffs[1].session?.board.first?.blockers == ["checks failing"])
        #expect(diffs[1].session?.board.first?.failingChecks.first?.url == "https://ci/1")
        #expect(diffs[2].event?.text == "Retry added.")
        #expect(diffs[6].subagent?.state == "stopped")
        #expect(diffs[7].removedSession == "s3")
    }

    @Test func everyGoldenStateFieldRoundTrips() throws {
        #expect(try roundTripMissingKeys(ViewState.self, golden: "view-subscribe-state.json") == [])
    }

    @Test func everyGoldenDiffFieldRoundTrips() throws {
        #expect(try roundTripMissingKeys([ViewDiff].self, golden: "view-subscribe-diffs.json") == [])
    }

    @Test func aRenamedFieldFailsToDecode() throws {
        let raw = try String(decoding: Goldens.data("view-subscribe-state.json"), as: UTF8.self)
            .replacingOccurrences(of: "\"WorktreeIDs\"", with: "\"Worktrees\"")
        #expect(throws: DecodingError.self) {
            try JSONDecoder().decode(ViewState.self, from: Data(raw.utf8))
        }
    }

    @Test func decodesAnUnavailableReplyAsATypedError() throws {
        let line = #"{"v":1,"id":0,"error":{"code":"unavailable","message":"no agentws daemon is running"}}"#
        let response = try JSONDecoder().decode(Response.self, from: Data(line.utf8))
        #expect(response.id == 0)
        #expect(response.error?.kind == .unavailable)
    }

    @Test func encodesARequestWithTheBuild() throws {
        let data = try Request(id: 3, method: "session.mute", params: ["id": "s1"], build: "v1.2.3").encoded()
        let object = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        #expect(object["v"] as? Int == 1)
        #expect(object["id"] as? Int == 3)
        #expect(object["method"] as? String == "session.mute")
        #expect(object["build"] as? String == "v1.2.3")
        #expect((object["params"] as? [String: String]) == ["id": "s1"])
    }
}
