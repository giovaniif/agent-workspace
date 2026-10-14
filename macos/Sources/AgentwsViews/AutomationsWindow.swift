#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

public enum AutomationTab: String, CaseIterable, Sendable {
    case overview = "Overview"
    case runs = "Runs"
}

public struct AutomationsScene {
    public var automations: [Automation]
    public var runs: [AutomationRun]
    public var problems: [String]
    public var query: AutomationQuery
    public var selected: String?
    public var tab: AutomationTab
    public var message: String?

    public init(
        automations: [Automation] = [], runs: [AutomationRun] = [], problems: [String] = [],
        query: AutomationQuery = AutomationQuery(), selected: String? = nil, tab: AutomationTab = .overview, message: String? = nil
    ) {
        self.automations = automations
        self.runs = runs
        self.problems = problems
        self.query = query
        self.selected = selected
        self.tab = tab
        self.message = message
    }

    public var rows: [AutomationRow] { AutomationTable.rows(automations, runs: runs, query: query) }

    public var selectedRow: AutomationRow? { rows.first { $0.id == selected } }

    public static func seeded(tab: AutomationTab = .overview) -> AutomationsScene {
        var fix = Automation(name: "fix-ci", agent: "claude", prompt: "Fix the CI of {{work_item}}.\nKeep the change small.", source: "~/.crew/config.toml")
        fix.on = ["pr.ci_failed"]
        fix.effort = "high"
        var deps = Automation(name: "deps", agent: "codex", prompt: "Update the dependencies and open a PR.", enabled: false, source: "~/.crew/config.toml")
        deps.schedule = "weekly mon 09:00"
        let audit = Automation(name: "audit", agent: "claude", prompt: "Audit the api for leaked secrets.", source: "/src/api/.agentws.toml")
        let runs = [
            AutomationRun.seeded("r3", "fix-ci", "event", "2026-10-13T10:05:00Z", "succeeded", session: "s3"),
            AutomationRun.seeded("r2", "deps", "schedule", "2026-10-13T09:00:00Z", "skipped", reason: "the last run is still live"),
            AutomationRun.seeded("r1", "fix-ci", "manual", "2026-10-12T08:00:00Z", "failed", reason: "no harness codex"),
        ]
        return AutomationsScene(automations: [fix, deps, audit], runs: runs, selected: fix.id, tab: tab)
    }
}

extension AutomationRun {
    static func seeded(_ id: String, _ name: String, _ trigger: String, _ start: String, _ outcome: String, session: String? = nil, reason: String? = nil) -> AutomationRun {
        var object: [String: String] = ["id": id, "automation": name, "trigger": trigger, "start": start, "outcome": outcome]
        if let session { object["session_id"] = session }
        if let reason { object["reason"] = reason }
        let data = (try? JSONSerialization.data(withJSONObject: object)) ?? Data()
        return (try? JSONDecoder().decode(AutomationRun.self, from: data))
            ?? AutomationRun.empty
    }

    static var empty: AutomationRun {
        let data = Data(#"{"id":"","automation":"","trigger":"","start":"","outcome":""}"#.utf8)
        return try! JSONDecoder().decode(AutomationRun.self, from: data)
    }
}

public struct AutomationsActions {
    public var query: (AutomationQuery) -> Void = { _ in }
    public var select: (String?) -> Void = { _ in }
    public var tab: (AutomationTab) -> Void = { _ in }
    public var run: (Automation) -> Void = { _ in }
    public var rerun: (AutomationRun) -> Void = { _ in }
    public var openSession: (String) -> Void = { _ in }
    public var edit: (Automation?) -> Void = { _ in }

    public init() {}
}

public struct AutomationsWindow: View {
    let scene: AutomationsScene
    let actions: AutomationsActions

    public init(scene: AutomationsScene, actions: AutomationsActions = AutomationsActions()) {
        self.scene = scene
        self.actions = actions
    }

    public var body: some View {
        HStack(spacing: 0) {
            listColumn.frame(width: 420)
            Divider()
            detailColumn.frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
    }

    private func setQuery(_ edit: (inout AutomationQuery) -> Void) {
        var q = scene.query
        edit(&q)
        actions.query(q)
    }

    private var filters: some View {
        VStack(alignment: .leading, spacing: 8) {
            TextField("Search name or prompt", text: Binding(get: { scene.query.search }, set: { v in setQuery { $0.search = v } }))
                .textFieldStyle(.roundedBorder)
            HStack {
                Picker("State", selection: Binding(
                    get: { scene.query.enabled.map { $0 ? "enabled" : "paused" } ?? "" },
                    set: { v in setQuery { $0.enabled = v.isEmpty ? nil : v == "enabled" } }
                )) {
                    Text("All").tag("")
                    Text("Enabled").tag("enabled")
                    Text("Paused").tag("paused")
                }
                Picker("Last", selection: Binding(
                    get: { scene.query.outcome ?? "" },
                    set: { v in setQuery { $0.outcome = v.isEmpty ? nil : v } }
                )) {
                    Text("Any").tag("")
                    ForEach(["succeeded", "failed", "skipped", "missed", "never run"], id: \.self) { Text($0).tag($0) }
                }
            }
            HStack {
                Picker("Agent", selection: Binding(
                    get: { scene.query.agent ?? "" },
                    set: { v in setQuery { $0.agent = v.isEmpty ? nil : v } }
                )) {
                    Text("Any").tag("")
                    ForEach(["claude", "codex", "omp"], id: \.self) { Text($0).tag($0) }
                }
                Picker("Sort", selection: Binding(get: { scene.query.sort }, set: { v in setQuery { $0.sort = v } })) {
                    Text("Name").tag(AutomationSort.name)
                    Text("Last run").tag(AutomationSort.lastRun)
                }
            }
        }
        .padding(12)
    }

    private var listColumn: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Automations").font(.title2.bold())
                Spacer()
                Button("New…") { actions.edit(nil) }
            }
            .padding([.horizontal, .top], 12)
            filters
            Divider()
            let rows = scene.rows
            if rows.isEmpty {
                Text(scene.automations.isEmpty ? "No automations. Add one with New…, or add [[automation]] to config.toml." : "No automation matches.")
                    .foregroundStyle(.secondary)
                    .padding(12)
                Spacer()
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(rows) { row in
                            AutomationRowView(row: row, selected: row.id == scene.selected)
                                .contentShape(Rectangle())
                                .onTapGesture { actions.select(row.id) }
                        }
                    }
                }
                .focusable()
                .onKeyPress(.upArrow) {
                    actions.select(AutomationTable.move(scene.selected, by: -1, in: rows))
                    return .handled
                }
                .onKeyPress(.downArrow) {
                    actions.select(AutomationTable.move(scene.selected, by: 1, in: rows))
                    return .handled
                }
                .onKeyPress(.return) {
                    actions.tab(.overview)
                    return .handled
                }
            }
            ForEach(scene.problems, id: \.self) { problem in
                Text(problem).font(.caption).foregroundStyle(.orange).padding(.horizontal, 12)
            }
        }
    }

    @ViewBuilder private var detailColumn: some View {
        if let row = scene.selectedRow {
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    Text(row.automation.name).font(.title2.bold())
                    Spacer()
                    if row.automation.repoFile == nil {
                        Button("Edit…") { actions.edit(row.automation) }
                    }
                    Button("Run…") { actions.run(row.automation) }
                        .keyboardShortcut("r", modifiers: .command)
                }
                Picker("", selection: Binding(get: { scene.tab }, set: { actions.tab($0) })) {
                    ForEach(AutomationTab.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(width: 220)
                switch scene.tab {
                case .overview: overview(row.automation)
                case .runs: runsList(row.automation)
                }
                if let message = scene.message {
                    Text(message).foregroundStyle(.secondary)
                }
                Spacer()
            }
            .padding(16)
        } else {
            Text("Select an automation").foregroundStyle(.secondary).padding(16)
        }
    }

    private func overview(_ a: Automation) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            fact("Agent", [a.agent, a.model, a.effort].compactMap { $0 }.joined(separator: " · "))
            fact("Target", a.targetLabel)
            fact("Trigger", a.trigger)
            if let tz = a.timezone { fact("Time zone", tz) }
            if let precheck = a.precheck { fact("Precheck", precheck) }
            fact("Session", a.session ?? "fresh")
            fact("State", a.enabled ? "enabled" : "paused")
            if let file = a.repoFile {
                fact("Defined in", file + " (edit it in the repo)")
            } else {
                fact("Defined in", a.source)
            }
            Text("Prompt").font(.headline).padding(.top, 8)
            Text(a.prompt)
                .font(.system(.body, design: .monospaced))
                .textSelection(.enabled)
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: 6).fill(Color.secondary.opacity(0.1)))
        }
    }

    private func fact(_ label: String, _ value: String) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(label).foregroundStyle(.secondary).frame(width: 90, alignment: .leading)
            Text(value).textSelection(.enabled)
        }
    }

    private func runsList(_ a: Automation) -> some View {
        let runs = AutomationTable.runs(of: a.name, in: scene.runs)
        return VStack(alignment: .leading, spacing: 4) {
            if runs.isEmpty {
                Text("No runs yet.").foregroundStyle(.secondary)
            }
            ForEach(runs) { run in
                HStack {
                    Text(AutomationTime.label(run.started)).font(.system(.body, design: .monospaced))
                    Text(run.trigger).foregroundStyle(.secondary).frame(width: 70, alignment: .leading)
                    Text(run.outcome).foregroundStyle(run.outcome == "succeeded" ? Color.green : Color.orange).frame(width: 80, alignment: .leading)
                    Text(run.reason ?? run.context ?? "").lineLimit(1).foregroundStyle(.secondary)
                    Spacer()
                    if let session = run.sessionID {
                        Button("Open session") { actions.openSession(session) }
                    }
                    Button("Rerun") { actions.rerun(run) }
                }
            }
        }
    }
}

struct AutomationRowView: View {
    let row: AutomationRow
    let selected: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(row.automation.name).font(.body.bold())
                Text(row.automation.agent).foregroundStyle(.secondary)
                Spacer()
                Text(row.automation.enabled ? "enabled" : "paused").font(.caption).foregroundStyle(row.automation.enabled ? Color.green : Color.secondary)
            }
            HStack {
                Text(row.automation.targetLabel)
                Text("·")
                Text(row.automation.trigger)
                Spacer()
                Text(row.lastOutcome + (row.lastTime.isEmpty ? "" : " · " + row.lastTime))
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(selected ? Color.accentColor.opacity(0.18) : Color.clear)
    }
}

public struct AutomationRunSheet: View {
    let automation: Automation
    let run: (String, String) -> Void
    let cancel: () -> Void
    @State private var context = ""
    @State private var workItem = ""

    public init(automation: Automation, run: @escaping (String, String) -> Void, cancel: @escaping () -> Void) {
        self.automation = automation
        self.run = run
        self.cancel = cancel
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Run \(automation.name)").font(.title3.bold())
            Text("Context").font(.headline)
            TextEditor(text: $context)
                .font(.system(.body, design: .monospaced))
                .frame(minHeight: 140)
                .overlay(RoundedRectangle(cornerRadius: 4).stroke(Color.secondary.opacity(0.3)))
            TextField("Work item (optional): a PR link, an issue or text", text: $workItem)
                .textFieldStyle(.roundedBorder)
            HStack {
                Spacer()
                Button("Cancel", role: .cancel) { cancel() }.keyboardShortcut(.cancelAction)
                Button("Run") { run(context, workItem) }.keyboardShortcut(.defaultAction)
            }
        }
        .padding(16)
        .frame(width: 520)
    }
}

public struct AutomationEditSheet: View {
    @State private var draft: AutomationDraft
    let configPath: String
    let save: (AutomationDraft) -> Void
    let cancel: () -> Void

    public init(draft: AutomationDraft, configPath: String, save: @escaping (AutomationDraft) -> Void, cancel: @escaping () -> Void) {
        _draft = State(initialValue: draft)
        self.configPath = configPath
        self.save = save
        self.cancel = cancel
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(draft.name.isEmpty ? "New automation" : "Edit \(draft.name)").font(.title3.bold())
            Form {
                TextField("Name", text: $draft.name)
                Picker("Agent", selection: $draft.agent) {
                    ForEach(["claude", "codex", "omp"], id: \.self) { Text($0).tag($0) }
                }
                TextField("Model", text: $draft.model)
                TextField("Effort", text: $draft.effort)
                TextField("Target", text: $draft.target)
                TextField("Schedule", text: $draft.schedule)
                TextField("Time zone", text: $draft.timezone)
                TextField("Events (comma separated)", text: $draft.on)
                TextField("Precheck", text: $draft.precheck)
                Picker("Session", selection: $draft.session) {
                    Text("fresh").tag("fresh")
                    Text("reuse").tag("reuse")
                }
                Toggle("Enabled", isOn: $draft.enabled)
            }
            Text("Prompt").font(.headline)
            TextEditor(text: $draft.prompt)
                .font(.system(.body, design: .monospaced))
                .frame(minHeight: 120)
                .overlay(RoundedRectangle(cornerRadius: 4).stroke(Color.secondary.opacity(0.3)))
            Text("Saved to \(configPath). crew keeps a backup of the old file.").font(.caption).foregroundStyle(.secondary)
            HStack {
                Spacer()
                Button("Cancel", role: .cancel) { cancel() }.keyboardShortcut(.cancelAction)
                Button("Save") { save(draft) }.keyboardShortcut(.defaultAction).disabled(draft.name.isEmpty || draft.prompt.isEmpty)
            }
        }
        .padding(16)
        .frame(width: 560)
    }
}
#endif
