#if canImport(SwiftUI)
import CrewKit
import SwiftUI

public struct MatesScene {
    public var board: MateBoard
    public var definitions: [MateDefinition]
    public var problems: [String]
    public var approvals: [ApprovalItem]
    public var message: String?

    public init(
        board: MateBoard = MateBoard(), definitions: [MateDefinition] = [], problems: [String] = [],
        approvals: [ApprovalItem] = [], message: String? = nil
    ) {
        self.board = board
        self.definitions = definitions
        self.problems = problems
        self.approvals = approvals
        self.message = message
    }

    public static func seeded() -> MatesScene {
        let thread = MateThread(
            mate: "helper", team: "T1", channel: "C1", threadTS: "1700.0001", sessionID: "s1",
            url: "https://app.slack.com/client/T1/C1/thread/C1-1700.0001", session: "fix the login redirect", state: "running"
        )
        let board = MateBoard(mates: [
            MateRow(name: "helper", state: "connected", work: ["s1"], threads: [thread]),
            MateRow(name: "reviewer", state: "connected", hold: "rate_limit"),
            MateRow(name: "webby", state: "paused"),
        ])
        return MatesScene(board: board, definitions: [
            MateDefinition(name: "helper", prompt: "You help with the api."),
            MateDefinition(name: "reviewer", prompt: "You review PRs."),
            MateDefinition(name: "webby", prompt: "You help with the web.", agent: "codex", enabled: false),
        ])
    }
}

public struct MatesActions {
    public var pause: (MateRow) -> Void = { _ in }
    public var stopAll: (Bool) -> Void = { _ in }
    public var edit: (MateDefinition?) -> Void = { _ in }
    public var connect: (String) -> Void = { _ in }
    public var openSession: (String) -> Void = { _ in }
    public var decide: (ApprovalItem, Bool) -> Void = { _, _ in }

    public init() {}
}

public struct MatesWindow: View {
    let scene: MatesScene
    let actions: MatesActions
    @State private var confirmStop = false
    @Environment(\.openURL) private var openURL

    public init(scene: MatesScene, actions: MatesActions = MatesActions()) {
        self.scene = scene
        self.actions = actions
    }

    public var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                HStack {
                    Text("Mates").font(.title2.bold())
                    Spacer()
                    Button("Add a mate…") { actions.edit(nil) }
                    if scene.board.stopped {
                        Button("Start all mates") { actions.stopAll(false) }
                    } else {
                        Button("Stop all mates", role: .destructive) { confirmStop = true }
                    }
                }
                if scene.board.stopped {
                    Label("All mates are stopped. No mate starts a new turn.", systemImage: "stop.circle.fill")
                        .foregroundStyle(.red)
                }
                if let message = scene.message {
                    Text(message).foregroundStyle(.orange)
                }
                ForEach(scene.problems, id: \.self) { problem in
                    Text(problem).font(.caption).foregroundStyle(.orange)
                }
                if scene.board.mates.isEmpty {
                    Text("No mates yet. Add one with Add a mate…").foregroundStyle(.secondary)
                }
                ForEach(scene.board.mates) { row in
                    mateCard(row)
                }
                Divider()
                Text("Approvals").font(.headline)
                if scene.approvals.isEmpty {
                    Text("No approvals wait for you.").foregroundStyle(.secondary)
                }
                ForEach(scene.approvals) { approval in
                    approvalCard(approval)
                }
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .confirmationDialog("Stop all mates?", isPresented: $confirmStop) {
            Button("Stop all", role: .destructive) { actions.stopAll(true) }
        } message: {
            Text("No mate starts a new turn until you start them again.")
        }
    }

    private func mateCard(_ row: MateRow) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(row.name).font(.headline)
                Text(row.label).foregroundStyle(row.hold != nil || row.state == "error" ? Color.orange : row.state == "connected" ? Color.green : Color.secondary)
                Spacer()
                Button(row.paused ? "Resume" : "Pause") { actions.pause(row) }
                if let def = scene.definitions.first(where: { $0.name == row.name }) {
                    Button("Settings…") { actions.edit(def) }
                }
                if row.state == "no_tokens" {
                    Button("Connect to Slack…") { actions.connect(row.name) }
                }
            }
            if let error = row.error {
                Text(error).font(.caption).foregroundStyle(.orange)
            }
            let work = row.currentWork
            Text(work.isEmpty ? "No current work." : "Works on " + work.map(\.label).joined(separator: ", "))
                .foregroundStyle(.secondary)
            ForEach(row.threads) { thread in
                HStack {
                    Text(thread.ended ? "ended" : thread.state).font(.caption).foregroundStyle(.secondary).frame(width: 70, alignment: .leading)
                    Button(thread.label) { actions.openSession(thread.sessionID) }.buttonStyle(.link)
                    if let url = URL(string: thread.url), !thread.url.isEmpty {
                        Button("Slack") { openURL(url) }.buttonStyle(.link)
                    }
                }
            }
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 8).fill(Color.secondary.opacity(0.08)))
    }

    private func approvalCard(_ approval: ApprovalItem) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(approval.title).font(.headline)
                Text("asked by " + (approval.requester.isEmpty ? "someone" : approval.requester)).foregroundStyle(.secondary)
                Spacer()
                Button("Open session") { actions.openSession(approval.sessionID) }
                Button("Deny", role: .destructive) { actions.decide(approval, false) }
                Button("Approve") { actions.decide(approval, true) }
            }
            Text(approval.action.tool + ": " + approval.action.input).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 8).fill(Color.secondary.opacity(0.08)))
    }
}

public struct MateEditSheet: View {
    @State private var draft: MateDraft
    let isNew: Bool
    let save: (MateDraft) -> Void
    let cancel: () -> Void

    public init(draft: MateDraft, isNew: Bool, save: @escaping (MateDraft) -> Void, cancel: @escaping () -> Void) {
        _draft = State(initialValue: draft)
        self.isNew = isNew
        self.save = save
        self.cancel = cancel
    }

    public var body: some View {
        Form {
            if isNew {
                TextField("Name", text: $draft.name)
            }
            Picker("Agent", selection: $draft.agent) {
                Text("claude").tag("claude")
                Text("codex").tag("codex")
            }
            TextField("Model", text: $draft.model)
            TextField("Effort", text: $draft.effort)
            TextField("Repos", text: $draft.repos, prompt: Text("api, web"))
            TextEditor(text: $draft.prompt).frame(minHeight: 120)
            HStack {
                Spacer()
                Button("Cancel", action: cancel).keyboardShortcut(.cancelAction)
                Button(isNew ? "Add mate" : "Save") { save(draft) }
                    .keyboardShortcut(.defaultAction)
                    .disabled(draft.name.trimmingCharacters(in: .whitespaces).isEmpty || draft.prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .padding(16)
        .frame(minWidth: 480)
    }
}

public struct MateConnectSheet: View {
    let name: String
    let setup: MateSetupLink?
    let claim: MateClaimCode?
    let message: String?
    let connect: (String, String) -> Void
    let done: () -> Void
    @State private var bot = ""
    @State private var app = ""
    @Environment(\.openURL) private var openURL

    public init(name: String, setup: MateSetupLink?, claim: MateClaimCode?, message: String? = nil, connect: @escaping (String, String) -> Void, done: @escaping () -> Void) {
        self.name = name
        self.setup = setup
        self.claim = claim
        self.message = message
        self.connect = connect
        self.done = done
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Connect \(name) to Slack").font(.title3.bold())
            if let setup, let url = URL(string: setup.url) {
                Button("1. Make the Slack app from the manifest") { openURL(url) }.buttonStyle(.link)
            }
            Text("2. Install the app in your workspace. A workspace admin may need to approve it first.")
            Text("3. Copy the bot token (xoxb-) from OAuth & Permissions.")
            Text("4. Make an app-level token (xapp-) with the scope connections:write in Basic Information, and copy it.")
            if let claim {
                Text("To become the owner of \(name), send this code to @\(name) in a direct message in Slack in the next 10 minutes:")
                Text(claim.code).font(.system(.title2, design: .monospaced)).textSelection(.enabled)
            } else {
                SecureField("Bot token", text: $bot)
                SecureField("App token", text: $app)
            }
            if let message {
                Text(message).foregroundStyle(.orange)
            }
            HStack {
                Spacer()
                Button("Done", action: done).keyboardShortcut(.cancelAction)
                if claim == nil {
                    Button("Save tokens") { connect(bot, app) }
                        .keyboardShortcut(.defaultAction)
                        .disabled(bot.isEmpty || app.isEmpty)
                }
            }
        }
        .padding(16)
        .frame(minWidth: 520)
    }
}
#endif
