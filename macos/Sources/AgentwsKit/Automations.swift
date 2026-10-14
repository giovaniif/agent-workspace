import Foundation
import Observation

public struct Automation: Codable, Sendable, Equatable, Identifiable {
    public var name: String
    public var agent: String
    public var model: String?
    public var effort: String?
    public var target: String?
    public var prompt: String
    public var schedule: String?
    public var timezone: String?
    public var on: [String]?
    public var precheck: String?
    public var session: String?
    public var enabled: Bool
    public var source: String
    public var trusted: Bool?

    public init(name: String, agent: String, prompt: String, enabled: Bool = true, source: String = "") {
        self.name = name
        self.agent = agent
        self.prompt = prompt
        self.enabled = enabled
        self.source = source
    }

    public var id: String { source + "\n" + name }

    public var trigger: String {
        var parts: [String] = []
        if let schedule, !schedule.isEmpty { parts.append(schedule) }
        if let on, !on.isEmpty { parts.append("on " + on.joined(separator: ", ")) }
        return parts.isEmpty ? "manual" : parts.joined(separator: " · ")
    }

    public var targetLabel: String {
        guard let target, !target.isEmpty else { return "new-worktree" }
        return target
    }

    public var repoFile: String? {
        source.hasSuffix("/.agentws.toml") ? source : nil
    }
}

public struct AutomationList: Codable, Sendable, Equatable {
    public var automations: [Automation]
    public var problems: [String]

    enum CodingKeys: String, CodingKey { case automations, problems }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        automations = try c.decodeIfPresent([Automation].self, forKey: .automations) ?? []
        problems = try c.decodeIfPresent([String].self, forKey: .problems) ?? []
    }
}

public struct AutomationRun: Codable, Sendable, Equatable, Identifiable {
    public var id: String
    public var automation: String
    public var trigger: String
    public var context: String?
    public var workItem: String?
    public var start: String
    public var outcome: String
    public var reason: String?
    public var sessionID: String?

    enum CodingKeys: String, CodingKey {
        case id, automation, trigger, context, start, outcome, reason
        case workItem = "work_item"
        case sessionID = "session_id"
    }

    public var started: Date? { AutomationTime.parse(start) }
}

public struct AutomationRuns: Codable, Sendable, Equatable {
    public var runs: [AutomationRun]

    enum CodingKeys: String, CodingKey { case runs }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        runs = try c.decodeIfPresent([AutomationRun].self, forKey: .runs) ?? []
    }
}

public enum AutomationTime {
    public static func parse(_ text: String) -> Date? {
        let plain = ISO8601DateFormatter()
        if let d = plain.date(from: text) { return d }
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fractional.date(from: text)
    }

    public static func label(_ date: Date?) -> String {
        guard let date else { return "" }
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm"
        return f.string(from: date)
    }
}

public enum AutomationSort: String, Sendable, CaseIterable {
    case name
    case lastRun
}

public struct AutomationQuery: Sendable, Equatable {
    public var search: String
    public var enabled: Bool?
    public var outcome: String?
    public var agent: String?
    public var sort: AutomationSort

    public init(search: String = "", enabled: Bool? = nil, outcome: String? = nil, agent: String? = nil, sort: AutomationSort = .name) {
        self.search = search
        self.enabled = enabled
        self.outcome = outcome
        self.agent = agent
        self.sort = sort
    }
}

public struct AutomationRow: Sendable, Equatable, Identifiable {
    public var automation: Automation
    public var last: AutomationRun?

    public var id: String { automation.id }
    public var lastOutcome: String { last?.outcome ?? "never run" }
    public var lastTime: String { AutomationTime.label(last?.started) }
}

public enum AutomationTable {
    public static func runs(of name: String, in runs: [AutomationRun]) -> [AutomationRun] {
        runs.filter { $0.automation == name }
            .sorted { ($0.started ?? .distantPast) > ($1.started ?? .distantPast) }
    }

    public static func rows(_ automations: [Automation], runs: [AutomationRun], query: AutomationQuery) -> [AutomationRow] {
        let needle = query.search.trimmingCharacters(in: .whitespaces).lowercased()
        var rows = automations.map { AutomationRow(automation: $0, last: Self.runs(of: $0.name, in: runs).first) }
        rows = rows.filter { row in
            let a = row.automation
            if !needle.isEmpty && !a.name.lowercased().contains(needle) && !a.prompt.lowercased().contains(needle) { return false }
            if let enabled = query.enabled, a.enabled != enabled { return false }
            if let outcome = query.outcome, row.lastOutcome != outcome { return false }
            if let agent = query.agent, a.agent != agent { return false }
            return true
        }
        switch query.sort {
        case .name:
            rows.sort { $0.automation.name < $1.automation.name }
        case .lastRun:
            rows.sort { x, y in
                let a = x.last?.started ?? .distantPast
                let b = y.last?.started ?? .distantPast
                return a == b ? x.automation.name < y.automation.name : a > b
            }
        }
        return rows
    }

    public static func move(_ selected: String?, by delta: Int, in rows: [AutomationRow]) -> String? {
        guard !rows.isEmpty else { return nil }
        guard let selected, let i = rows.firstIndex(where: { $0.id == selected }) else { return rows[0].id }
        return rows[min(max(i + delta, 0), rows.count - 1)].id
    }
}

public struct AutomationDraft: Sendable, Equatable {
    public var name = ""
    public var agent = "claude"
    public var model = ""
    public var effort = ""
    public var target = "new-worktree"
    public var prompt = ""
    public var schedule = ""
    public var timezone = ""
    public var on = ""
    public var precheck = ""
    public var session = "fresh"
    public var enabled = true

    public init() {}

    public init(_ a: Automation) {
        name = a.name
        agent = a.agent
        model = a.model ?? ""
        effort = a.effort ?? ""
        target = a.targetLabel
        prompt = a.prompt
        schedule = a.schedule ?? ""
        timezone = a.timezone ?? ""
        on = (a.on ?? []).joined(separator: ", ")
        precheck = a.precheck ?? ""
        session = a.session ?? "fresh"
        enabled = a.enabled
    }

    public var automation: Automation {
        func value(_ s: String) -> String? {
            let t = s.trimmingCharacters(in: .whitespacesAndNewlines)
            return t.isEmpty ? nil : t
        }
        var a = Automation(name: name.trimmingCharacters(in: .whitespaces), agent: agent, prompt: prompt, enabled: enabled)
        a.model = value(model)
        a.effort = value(effort)
        a.target = value(target)
        a.schedule = value(schedule)
        a.timezone = value(timezone)
        let events = on.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        a.on = events.isEmpty ? nil : events
        a.precheck = value(precheck)
        a.session = value(session)
        return a
    }
}

struct AutomationRunParams: Encodable, Sendable {
    var name: String?
    var context: String?
    var workItem: String?
    var rerun: String?

    enum CodingKeys: String, CodingKey {
        case name, context, rerun
        case workItem = "work_item"
    }
}

struct AutomationRunResult: Decodable, Sendable {
    var run: AutomationRun
}

struct AutomationRunsParams: Encodable, Sendable {
    var limit: Int
}

struct AutomationSaveParams: Encodable, Sendable {
    var automation: Automation
}

struct EmptyParams: Encodable, Sendable {}

@MainActor
@Observable
public final class AutomationsModel {
    public private(set) var list: [Automation] = []
    public private(set) var problems: [String] = []
    public private(set) var runs: [AutomationRun] = []
    public var message: String?
    private let caller: Caller

    public init(caller: Caller) {
        self.caller = caller
    }

    public func load() async {
        do {
            let l: AutomationList = try await caller.call("automation.list", params: EmptyParams())
            let r: AutomationRuns = try await caller.call("automation.runs", params: AutomationRunsParams(limit: 200))
            list = l.automations
            problems = l.problems
            runs = r.runs
        } catch {
            message = ReviewController.message(error)
        }
    }

    public func run(_ name: String, context: String, workItem: String) async -> AutomationRun? {
        let params = AutomationRunParams(
            name: name,
            context: context.isEmpty ? nil : context,
            workItem: workItem.isEmpty ? nil : workItem
        )
        return await start(params)
    }

    public func rerun(_ runID: String) async -> AutomationRun? {
        await start(AutomationRunParams(rerun: runID))
    }

    private func start(_ params: AutomationRunParams) async -> AutomationRun? {
        do {
            let out: AutomationRunResult = try await caller.call("automation.run", params: params)
            runs.insert(out.run, at: 0)
            if out.run.outcome == "succeeded" {
                message = nil
            } else {
                message = "\(out.run.automation): \(out.run.outcome)" + (out.run.reason.map { " (\($0))" } ?? "")
            }
            return out.run
        } catch {
            message = ReviewController.message(error)
            return nil
        }
    }

    public func save(_ draft: AutomationDraft) async -> Bool {
        do {
            let _: Automation = try await caller.call("automation.save", params: AutomationSaveParams(automation: draft.automation))
            message = nil
        } catch {
            message = ReviewController.message(error)
            return false
        }
        await load()
        return true
    }
}
