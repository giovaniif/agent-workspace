#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

@MainActor
public struct SettingsActions {
    public var select: (SettingsTab) -> Void = { _ in }
    public var update: (AppSettings) -> Void = { _ in }
    public var assign: (ShortcutAction, KeyCombo?) -> Void = { _, _ in }
    public var restoreShortcut: (ShortcutAction) -> Void = { _ in }
    public var restoreShortcuts: () -> Void = {}
    public var linkCLI: () -> Void = {}
    public var unlinkCLI: () -> Void = {}
    public var installHooks: (String) -> Void = { _ in }
    public var removeHooks: (String) -> Void = { _ in }
    public var installNvim: () -> Void = {}
    public var addWorkspace: (String) -> Void = { _ in }
    public var removeWorkspace: (String) -> Void = { _ in }
    public var refresh: () -> Void = {}
    public var setConfig: (String, String) -> Void = { _, _ in }
    public var revokeDevice: (String) -> Void = { _ in }
    public var pairPhone: () -> Void = {}

    public init() {}
}

public struct SettingsView: View {
    let scene: SettingsScene
    let actions: SettingsActions
    @Environment(\.colorScheme) private var scheme

    public init(scene: SettingsScene, actions: SettingsActions = SettingsActions()) {
        self.scene = scene
        self.actions = actions
    }

    public var body: some View {
        let theme = Theme(scheme)
        VStack(spacing: 0) {
            tabs(theme)
            Divider()
            if let error = scene.error {
                banner(error, tone: .red, theme)
            } else if let notice = scene.notice {
                banner(notice, tone: .green, theme)
            }
            ScrollView {
                content
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(20)
            }
        }
        .background(theme(.base))
        .foregroundStyle(theme(.text))
    }

    private func tabs(_ theme: Theme) -> some View {
        HStack(spacing: 4) {
            ForEach(SettingsTab.allCases) { tab in
                Button { actions.select(tab) } label: {
                    VStack(spacing: 3) {
                        Image(systemName: tab.symbol).font(.system(size: 18))
                        Text(tab.title).font(.system(size: 11))
                    }
                    .frame(width: 92, height: 50)
                    .background(tab == scene.tab ? theme(.surface) : .clear, in: RoundedRectangle(cornerRadius: Metrics.corner))
                    .foregroundStyle(tab == scene.tab ? theme(.blue) : theme(.subtext))
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity)
        .background(theme(.mantle))
    }

    private func banner(_ text: String, tone: Tone, _ theme: Theme) -> some View {
        Text(text)
            .font(.system(size: 12))
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 20)
            .padding(.vertical, 6)
            .background(theme(tone).opacity(0.15))
            .foregroundStyle(theme(tone))
    }

    @ViewBuilder
    private var content: some View {
        switch scene.tab {
        case .general: GeneralPane(scene: scene, actions: actions)
        case .workspaces: WorkspacesPane(scene: scene, actions: actions)
        case .agents: AgentsPane(scene: scene, actions: actions)
        case .notifications: NotificationsPane(scene: scene, actions: actions)
        case .appearance: AppearancePane(scene: scene, actions: actions)
        case .shortcuts: ShortcutsPane(scene: scene, actions: actions)
        }
    }
}

@MainActor
func binding<Value>(_ scene: SettingsScene, _ actions: SettingsActions, _ path: WritableKeyPath<AppSettings, Value>) -> Binding<Value> {
    Binding(
        get: { scene.settings[keyPath: path] },
        set: { value in
            var settings = scene.settings
            settings[keyPath: path] = value
            actions.update(settings)
        }
    )
}

struct SettingsGroup<Content: View>: View {
    let title: String
    @ViewBuilder let content: Content
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.system(size: 12, weight: .semibold)).foregroundStyle(Theme(scheme)(.subtext))
            VStack(alignment: .leading, spacing: 8) { content }
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme(scheme)(.mantle), in: RoundedRectangle(cornerRadius: Metrics.corner))
        }
        .padding(.bottom, 14)
    }
}

struct GeneralPane: View {
    let scene: SettingsScene
    let actions: SettingsActions

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            SettingsGroup(title: "Startup") {
                Toggle("Open at login", isOn: binding(scene, actions, \.general.openAtLogin))
                TextField("Server to connect at launch", text: binding(scene, actions, \.general.launchServer))
                Toggle("Check for updates", isOn: binding(scene, actions, \.general.checkUpdates))
            }
            SettingsGroup(title: "Menu bar and Dock") {
                Picker("Menu bar icon counts", selection: binding(scene, actions, \.general.menuBarCount)) {
                    ForEach(BadgeCount.allCases, id: \.self) { Text($0.title).tag($0) }
                }
                Picker("Dock badge counts", selection: binding(scene, actions, \.general.dockBadge)) {
                    ForEach(BadgeCount.allCases, id: \.self) { Text($0.title).tag($0) }
                }
            }
            SettingsGroup(title: "Sessions") {
                Stepper("Ended sessions shown: \(scene.settings.general.endedShown)", value: binding(scene, actions, \.general.endedShown), in: 0...100, step: 5)
                Toggle("Confirm before ending a session", isOn: binding(scene, actions, \.general.confirmOnEnd))
                Toggle("Show worktrees", isOn: binding(scene, actions, \.general.showWorktrees))
                Toggle("Show subagents", isOn: binding(scene, actions, \.general.showSubagents))
            }
            SettingsGroup(title: "Command line") {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("agentws on PATH")
                        Text(cliText).font(Metrics.mono).foregroundStyle(.secondary)
                    }
                    Spacer()
                    if scene.cliLink == .linked {
                        Button("Remove link", action: actions.unlinkCLI)
                    } else {
                        Button("Link", action: actions.linkCLI).disabled(scene.cliLink != .missing)
                    }
                }
            }
        }
    }

    private var cliText: String {
        switch scene.cliLink {
        case .linked: "\(CLILink.standardPath) → this app's agentws"
        case .missing: "\(CLILink.standardPath) is not set up"
        case .elsewhere(let other): "\(CLILink.standardPath) points at \(other)"
        }
    }
}

struct WorkspacesPane: View {
    let scene: SettingsScene
    let actions: SettingsActions
    @State private var path = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("On \(scene.server)").font(.system(size: 12)).foregroundStyle(.secondary).padding(.bottom, 10)
            SettingsGroup(title: "Workspaces") {
                if scene.workspaces.isEmpty {
                    Text("No workspaces yet").foregroundStyle(.secondary)
                }
                ForEach(scene.workspaces) { workspace in
                    HStack(alignment: .top) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(workspace.root).font(Metrics.mono)
                            Text(workspace.summary).font(.system(size: 11)).foregroundStyle(.secondary)
                            if workspace.kind == "orchestration" {
                                Text(workspace.repos.map(\.name).joined(separator: " · ")).font(.system(size: 11)).foregroundStyle(.secondary)
                            }
                        }
                        Spacer()
                        Button("Remove") { actions.removeWorkspace(workspace.root) }
                    }
                }
                HStack {
                    TextField("Path on the server", text: $path).font(Metrics.mono)
                    Button("Add") {
                        actions.addWorkspace(path)
                        path = ""
                    }
                    .disabled(path.isEmpty)
                }
            }
            ServerConfigGroup(title: "Worktrees and launcher", scene: scene, actions: actions, fields: ConfigFields.workspaces) {
                if let config = scene.config {
                    LabeledContent("Worktree location") { Text(config.worktreeLocation).font(Metrics.mono) }
                    LabeledContent("Auto-cleanup") { Text("Every 10 minutes and when a PR merges") }
                }
            }
        }
    }
}

struct ServerConfigGroup<Extra: View>: View {
    let title: String
    let scene: SettingsScene
    let actions: SettingsActions
    let fields: [ConfigField]
    @ViewBuilder var extra: Extra

    init(title: String, scene: SettingsScene, actions: SettingsActions, fields: [ConfigField], @ViewBuilder extra: () -> Extra = { EmptyView() }) {
        self.title = title
        self.scene = scene
        self.actions = actions
        self.fields = fields
        self.extra = extra()
    }

    var body: some View {
        SettingsGroup(title: title) {
            extra
            if let config = scene.config {
                ForEach(fields) { field in
                    ConfigFieldRow(field: field, value: config.value(field.key)) { actions.setConfig(field.key, $0) }
                }
                Text("Saved to \(config.path); the old file is backed up first.").font(.system(size: 11)).foregroundStyle(.secondary)
            } else {
                Text("This server's agentws cannot change config.toml yet. Update it to edit these here.").font(.system(size: 11)).foregroundStyle(.secondary)
            }
        }
    }
}

struct ConfigFieldRow: View {
    let field: ConfigField
    let value: String
    let save: (String) -> Void
    @State private var text = ""

    var body: some View {
        if field.choices.isEmpty {
            HStack {
                Text(field.title)
                Spacer()
                TextField(field.placeholder, text: $text)
                    .font(Metrics.mono)
                    .frame(width: 140)
                    .onSubmit { if text != value { save(text) } }
                    .onAppear { text = value }
                    .onChange(of: value) { _, new in text = new }
            }
        } else {
            Picker(field.title, selection: Binding(get: { value }, set: { if $0 != value { save($0) } })) {
                Text(field.placeholder).tag("")
                ForEach(choices, id: \.self) { Text($0).tag($0) }
            }
        }
    }

    private var choices: [String] {
        field.choices.contains(value) || value.isEmpty ? field.choices : field.choices + [value]
    }
}

struct AgentsPane: View {
    let scene: SettingsScene
    let actions: SettingsActions
    @State private var removing: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            panes
        }
        .confirmationDialog(
            "Remove the agentws hooks?",
            isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
            presenting: removing
        ) { key in
            Button("Back up and remove", role: .destructive) { actions.removeHooks(key) }
        } message: { _ in
            Text("The settings file is copied to a backup first. Install hooks puts them back.")
        }
    }

    @ViewBuilder
    private var panes: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("On \(scene.server)").font(.system(size: 12)).foregroundStyle(.secondary).padding(.bottom, 10)
            if let agents = scene.agents {
                SettingsGroup(title: "Hooks") {
                    harness("Claude Code", "claude", agents.harnesses["claude"])
                    harness("Codex", "codex", agents.harnesses["codex"])
                }
                ForEach(scene.harnesses.filter { $0.harness != "omp" }, id: \.harness) { h in
                    ServerConfigGroup(title: "\(h.name) new sessions", scene: scene, actions: actions, fields: ConfigFields.defaults(for: h.harness, models: h.models, efforts: h.efforts))
                }
                ServerConfigGroup(title: "Quota", scene: scene, actions: actions, fields: ConfigFields.agents) {
                    LabeledContent("Limits count as stale after") { Text("15 minutes") }
                }
                SettingsGroup(title: "nvim") {
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(agents.nvim.onPath ? "nvim is on PATH" : "nvim is not on PATH")
                            Text(agents.nvim.configured ? "Plugin set up in \(agents.nvim.configFile)" : "Plugin not set up")
                                .font(.system(size: 11)).foregroundStyle(.secondary)
                        }
                        Spacer()
                        if !agents.nvim.configured {
                            Button("Set up plugin", action: actions.installNvim).disabled(!agents.nvim.pluginFound)
                        }
                    }
                }
            } else {
                Text("Reading the server…").foregroundStyle(.secondary)
            }
        }
    }

    private func harness(_ title: String, _ key: String, _ setup: HarnessSetup?) -> some View {
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                if let setup {
                    Text(setup.installed ? "Installed in \(setup.file)" : "Not installed").font(.system(size: 11)).foregroundStyle(.secondary)
                    if key == "codex", setup.installed {
                        Text("Codex runs a hook only after you trust it: accept the review prompt, or open /hooks.").font(.system(size: 11)).foregroundStyle(.secondary)
                    }
                    if let err = setup.err, !err.isEmpty {
                        Text(err).font(.system(size: 11)).foregroundStyle(.red)
                    }
                }
            }
            Spacer()
            if setup?.installed == true {
                Button("Remove hooks…") { removing = key }
            } else {
                Button("Install hooks") { actions.installHooks(key) }
            }
        }
    }
}

struct NotificationsPane: View {
    let scene: SettingsScene
    let actions: SettingsActions

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            SettingsGroup(title: "Events") {
                Grid(alignment: .leading, horizontalSpacing: 24, verticalSpacing: 8) {
                    GridRow {
                        Text("")
                        Text("Banner").font(.system(size: 11)).foregroundStyle(.secondary)
                        Text("Sound").font(.system(size: 11)).foregroundStyle(.secondary)
                    }
                    row("Needs permission", \.notifications.permission)
                    row("Waiting for you", \.notifications.waiting)
                    row("Done", \.notifications.done)
                    row("Limit or API error", \.notifications.limitOrError)
                }
            }
            SettingsGroup(title: "When") {
                Toggle("Skip the session in view while the app is frontmost", isOn: binding(scene, actions, \.notifications.skipSessionInView))
                Toggle("Notify for muted sessions", isOn: binding(scene, actions, \.notifications.notifyMuted))
            }
            ServerConfigGroup(title: "Phones on \(scene.server)", scene: scene, actions: actions, fields: ConfigFields.notifications) {
                if scene.devices.isEmpty {
                    Text("No phones paired").foregroundStyle(.secondary)
                }
                ForEach(scene.devices) { device in
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(device.name)
                            Text("Paired \(device.createdAt.prefix(10)) · last seen \(device.lastSeen.prefix(10))").font(.system(size: 11)).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Button("Revoke") { actions.revokeDevice(device.id) }
                    }
                }
                HStack {
                    if let pairing = scene.pairing {
                        Text("Code \(pairing.code)").font(Metrics.mono)
                        Text("Type it on the agentws page on your phone before \(pairing.expiresAt.dropFirst(11).prefix(5)) UTC").font(.system(size: 11)).foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("Pair a phone", action: actions.pairPhone)
                }
            }
        }
    }

    private func row(_ title: String, _ path: WritableKeyPath<AppSettings, EventAlert>) -> some View {
        GridRow {
            Text(title)
            Toggle("", isOn: binding(scene, actions, path.appending(path: \.banner))).labelsHidden()
            Toggle("", isOn: binding(scene, actions, path.appending(path: \.sound))).labelsHidden()
        }
    }
}

struct AppearancePane: View {
    let scene: SettingsScene
    let actions: SettingsActions
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            SettingsGroup(title: "Terminal preview") {
                TerminalPreview(appearance: scene.settings.appearance, palette: scene.settings.appearance.theme.palette(systemDark: scheme == .dark))
            }
            SettingsGroup(title: "Theme") {
                Picker("Colours", selection: binding(scene, actions, \.appearance.theme)) {
                    ForEach(ThemeChoice.allCases, id: \.self) { Text($0.title).tag($0) }
                }
                .pickerStyle(.segmented)
                Picker("Accent", selection: binding(scene, actions, \.appearance.accent)) {
                    ForEach(AccentChoice.allCases, id: \.self) { Text($0.rawValue.capitalized).tag($0) }
                }
                Picker("Sidebar density", selection: binding(scene, actions, \.appearance.density)) {
                    ForEach(Density.allCases, id: \.self) { Text($0.rawValue.capitalized).tag($0) }
                }
                Picker("Default diff layout", selection: binding(scene, actions, \.appearance.diffLayout)) {
                    ForEach(DiffLayout.allCases, id: \.self) { Text($0.rawValue.capitalized).tag($0) }
                }
            }
            SettingsGroup(title: "Terminal") {
                TextField("Font", text: binding(scene, actions, \.appearance.terminalFont))
                Stepper("Size: \(Int(scene.settings.appearance.fontSize)) pt", value: binding(scene, actions, \.appearance.fontSize), in: Appearance.fontSizes, step: 1)
                Stepper(String(format: "Line height: %.1f", scene.settings.appearance.lineHeight), value: binding(scene, actions, \.appearance.lineHeight), in: Appearance.lineHeights, step: 0.1)
                Picker("Cursor", selection: binding(scene, actions, \.appearance.cursor)) {
                    ForEach(CursorShape.allCases, id: \.self) { Text($0.rawValue.capitalized).tag($0) }
                }
                .pickerStyle(.segmented)
                Toggle("Cursor blinks", isOn: binding(scene, actions, \.appearance.cursorBlinks))
            }
            ServerConfigGroup(title: "TUI [theme] overrides on \(scene.server)", scene: scene, actions: actions, fields: ConfigFields.theme)
        }
    }
}

struct TerminalPreview: View {
    let appearance: Appearance
    let palette: Palette

    var body: some View {
        let font = Font.custom(appearance.terminalFont, size: appearance.fontSize).monospaced()
        let spacing = (appearance.lineHeight - 1) * appearance.fontSize
        VStack(alignment: .leading, spacing: spacing) {
            line([("~/code/platform/api", .blue), (" on ", .subtext), ("42-retry", .green)], font)
            line([("❯ ", .peach), ("npm test", .text)], font)
            line([("  ✓ ", .green), ("12 passed", .text), (" · 0.8s", .subtext)], font)
            HStack(spacing: 0) {
                Text("❯ ").font(font).foregroundStyle(Color(hex: palette.hex(.peach)))
                cursor
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, minHeight: 140, alignment: .topLeading)
        .background(Color(hex: palette.hex(.base)), in: RoundedRectangle(cornerRadius: Metrics.corner))
        .overlay(RoundedRectangle(cornerRadius: Metrics.corner).stroke(Color(hex: palette.hex(.surface))))
    }

    private func line(_ parts: [(String, Tone)], _ font: Font) -> some View {
        HStack(spacing: 0) {
            ForEach(Array(parts.enumerated()), id: \.offset) { _, part in
                Text(part.0).font(font).foregroundStyle(Color(hex: palette.hex(part.1)))
            }
        }
    }

    @ViewBuilder
    private var cursor: some View {
        let colour = Color(hex: palette.hex(.text))
        let width = appearance.fontSize * 0.6
        switch appearance.cursor {
        case .block: Rectangle().fill(colour).frame(width: width, height: appearance.fontSize * 1.2)
        case .bar: Rectangle().fill(colour).frame(width: 2, height: appearance.fontSize * 1.2)
        case .underline: Rectangle().fill(colour).frame(width: width, height: 2).frame(height: appearance.fontSize * 1.2, alignment: .bottom)
        }
    }
}

struct ShortcutsPane: View {
    let scene: SettingsScene
    let actions: SettingsActions
    @State private var recording: ShortcutAction?
    @FocusState private var focus: ShortcutAction?
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        VStack(alignment: .leading, spacing: 0) {
            SettingsGroup(title: "Terminal") {
                Toggle("Pass keys through to a focused terminal (only ⌘ shortcuts reach the app)", isOn: binding(scene, actions, \.shortcuts.passThrough))
            }
            if let refusal = scene.refusal {
                Text(refusal)
                    .font(.system(size: 12, weight: .medium))
                    .foregroundStyle(theme(.red))
                    .padding(8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(theme(.red).opacity(0.12), in: RoundedRectangle(cornerRadius: Metrics.corner))
                    .padding(.bottom, 10)
            }
            SettingsGroup(title: "Actions") {
                Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 6) {
                    GridRow {
                        Text("Action").font(.system(size: 11)).foregroundStyle(.secondary)
                        Text("Mac").font(.system(size: 11)).foregroundStyle(.secondary)
                        Text("TUI").font(.system(size: 11)).foregroundStyle(.secondary)
                        Text("")
                    }
                    GridRow {
                        Text(Shortcuts.sessionJumps)
                        Text("⌘1–⌘9").font(Metrics.mono)
                        Text("1–9").font(Metrics.mono).foregroundStyle(.secondary)
                        Text("")
                    }
                    ForEach(ShortcutAction.allCases) { action in
                        GridRow {
                            Text(action.title)
                            recorder(action, theme)
                            Text(action.tuiKey ?? "—").font(Metrics.mono).foregroundStyle(.secondary)
                            Button("Default") { actions.restoreShortcut(action) }
                                .disabled(scene.settings.shortcuts.combo(for: action) == action.defaultCombo)
                        }
                    }
                }
                HStack {
                    Spacer()
                    Button("Restore defaults", action: actions.restoreShortcuts)
                }
            }
        }
    }

    private func recorder(_ action: ShortcutAction, _ theme: Theme) -> some View {
        let combo = scene.settings.shortcuts.combo(for: action)
        let isRecording = recording == action
        return Button {
            recording = isRecording ? nil : action
            focus = recording
        } label: {
            Text(isRecording ? "Type a shortcut…" : combo?.display ?? "None")
                .font(Metrics.mono)
                .frame(width: 120, alignment: .leading)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(isRecording ? theme(.blue).opacity(0.15) : theme(.surface).opacity(0.5), in: RoundedRectangle(cornerRadius: 4))
        }
        .buttonStyle(.plain)
        .focusable()
        .focused($focus, equals: action)
        .onKeyPress(phases: .down) { press in
            guard isRecording else { return .ignored }
            if press.key == .escape {
                recording = nil
                return .handled
            }
            if press.key == .delete && press.modifiers.isEmpty {
                actions.assign(action, nil)
                recording = nil
                return .handled
            }
            actions.assign(action, KeyCombo(press))
            recording = nil
            return .handled
        }
    }
}

extension KeyCombo {
    init(_ press: KeyPress) {
        var modifiers: Modifiers = []
        if press.modifiers.contains(.command) { modifiers.insert(.command) }
        if press.modifiers.contains(.control) { modifiers.insert(.control) }
        if press.modifiers.contains(.option) { modifiers.insert(.option) }
        if press.modifiers.contains(.shift) { modifiers.insert(.shift) }
        let key: String
        switch press.key {
        case .space: key = "space"
        case .delete: key = "delete"
        case .return: key = "return"
        case .tab: key = "tab"
        default: key = press.key.character.lowercased()
        }
        self.init(key, modifiers)
    }

    var equivalent: KeyEquivalent {
        switch key {
        case "space": .space
        case "delete": .delete
        case "return": .return
        case "tab": .tab
        case "escape": .escape
        default: KeyEquivalent(key.first ?? " ")
        }
    }

    public var keyboardShortcut: KeyboardShortcut { KeyboardShortcut(equivalent, modifiers: eventModifiers) }

    var eventModifiers: EventModifiers {
        var out: EventModifiers = []
        if modifiers.contains(.command) { out.insert(.command) }
        if modifiers.contains(.control) { out.insert(.control) }
        if modifiers.contains(.option) { out.insert(.option) }
        if modifiers.contains(.shift) { out.insert(.shift) }
        return out
    }
}
#endif
