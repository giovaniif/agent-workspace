import Foundation
import Observation

public struct DirChild: Codable, Equatable, Sendable {
    public var name: String
    public var path: String
    public var git: Int

    enum CodingKeys: String, CodingKey {
        case name = "Name"
        case path = "Path"
        case git = "Git"
    }

    public init(name: String, path: String, git: Int) {
        self.name = name
        self.path = path
        self.git = git
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.decode(String.self, forKey: .path)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? URL(fileURLWithPath: path).lastPathComponent
        git = try c.decodeIfPresent(Int.self, forKey: .git) ?? 0
    }

    public var mark: String {
        switch git {
        case 1: "repo"
        case 2: "worktree"
        default: ""
        }
    }
}

public struct ParsedPath: Equatable, Sendable {
    public var dir: String
    public var prefix: String
    public var path: String

    public init(dir: String, prefix: String, path: String) {
        self.dir = dir
        self.prefix = prefix
        self.path = path
    }

    public static func parse(_ text: String, home: String) -> ParsedPath {
        var input = text
        if input == "~" {
            input = home + "/"
        } else if input.hasPrefix("~/") {
            input = home + input.dropFirst()
        } else if !input.hasPrefix("/") {
            input = home + "/" + input
        }
        let cut = input.lastIndex(of: "/").map { input.index(after: $0) } ?? input.startIndex
        return ParsedPath(dir: clean(String(input[..<cut])), prefix: String(input[cut...]), path: clean(input))
    }

    static func clean(_ path: String) -> String {
        var parts: [Substring] = []
        for part in path.split(separator: "/") {
            switch part {
            case ".": continue
            case "..": if !parts.isEmpty { parts.removeLast() }
            default: parts.append(part)
            }
        }
        return "/" + parts.joined(separator: "/")
    }
}

@MainActor
@Observable
public final class WorkspacePathInput {
    public var text = "" {
        didSet { if text != oldValue { highlight = 0 } }
    }
    public var home = ""
    public private(set) var highlight = 0
    private var listed: String?
    private var children: [DirChild] = []
    private var missing = false

    @ObservationIgnored private let caller: any Caller

    public init(caller: any Caller) {
        self.caller = caller
    }

    public convenience init() {
        self.init(caller: OfflineCaller())
    }

    public var parsed: ParsedPath { ParsedPath.parse(text, home: home) }

    public var resolved: String { text.isEmpty ? "" : parsed.path }

    public var suggestions: [DirChild] {
        guard !text.isEmpty, listed == parsed.dir, !missing else { return [] }
        let prefix = parsed.prefix
        let lower = prefix.lowercased()
        return children
            .filter { $0.name.hasPrefix(".") == prefix.hasPrefix(".") && $0.name.lowercased().hasPrefix(lower) }
            .sorted { $0.name.lowercased() < $1.name.lowercased() }
    }

    public var status: String {
        guard !text.isEmpty else { return "" }
        let p = parsed
        guard listed == p.dir else { return p.path }
        if missing { return "\(p.path) · no such folder" }
        if p.prefix.isEmpty {
            let repos = children.filter { $0.git == 1 }.count
            return repos > 0 ? "\(p.path) · orchestration root · \(repos) repos" : "\(p.path) · folder"
        }
        guard let match = children.first(where: { $0.name == p.prefix }) else { return "\(p.path) · no such folder" }
        switch match.git {
        case 1: return "\(p.path) · single repo"
        case 2: return "\(p.path) · worktree"
        default: return "\(p.path) · folder"
        }
    }

    public func refresh() async {
        guard !text.isEmpty else { return }
        let dir = parsed.dir
        guard dir != listed else { return }
        do {
            let reply: DirsReply = try await caller.call("workspace.dirs", params: ["path": dir])
            guard parsed.dir == dir else { return }
            children = reply.dirs
            missing = false
        } catch {
            guard parsed.dir == dir else { return }
            children = []
            missing = true
        }
        listed = dir
        highlight = 0
    }

    public func moveDown() {
        highlight = min(highlight + 1, max(suggestions.count - 1, 0))
    }

    public func moveUp() {
        highlight = max(highlight - 1, 0)
    }

    @discardableResult
    public func accept(_ index: Int? = nil) -> Bool {
        let list = suggestions
        let chosen = index ?? highlight
        guard list.indices.contains(chosen) else { return false }
        let head = text.lastIndex(of: "/").map { String(text[...$0]) } ?? ""
        text = head + list[chosen].name + "/"
        highlight = 0
        return true
    }
}
