#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

public struct NewSessionSheet: View {
    @Bindable var model: NewSession
    @Binding var tab: NewSessionTab
    let server: String
    let state: ViewState?
    let live: Bool
    let started: (String) -> Void
    let cancel: () -> Void
    @Environment(\.colorScheme) private var scheme

    public init(
        model: NewSession, tab: Binding<NewSessionTab>, server: String, state: ViewState?, live: Bool = true,
        started: @escaping (String) -> Void = { _ in }, cancel: @escaping () -> Void = {}
    ) {
        self.model = model
        _tab = tab
        self.server = server
        self.state = state
        self.live = live
        self.started = started
        self.cancel = cancel
    }

    public var body: some View {
        let theme = Theme(scheme)
        VStack(alignment: .leading, spacing: 14) {
            Picker("", selection: $tab) {
                Text("New session").tag(NewSessionTab.session)
                Text("Linear launcher").tag(NewSessionTab.launcher)
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            if let error = model.loadError {
                Notice(text: error, tone: .red)
            }
            VStack(alignment: .leading, spacing: 10) {
                row("Server") {
                    HStack(spacing: 6) {
                        Circle().fill(theme(.green)).frame(width: 7, height: 7)
                        Text(server)
                    }
                }
                row("Workspace") { workspacePicker }
                if tab == .session {
                    row("Work item") { workItem(theme) }
                } else {
                    row("Issues") { launchInput(theme) }
                }
                row("Agent") { harnessPicker }
                row("Model") { modelPicker }
                row("Effort") { effortPicker }
            }
            if let warning = model.warning {
                HStack(spacing: 8) {
                    Image(systemName: "gauge.with.dots.needle.33percent").foregroundStyle(theme(.peach))
                    Text(warning.message).font(.system(size: 12))
                    Spacer()
                    if let button = warning.button {
                        Button(button) { model.takeSwitch() }
                    }
                }
                .padding(10)
                .background(RoundedRectangle(cornerRadius: Metrics.corner).fill(theme(.peach).opacity(0.14)))
                .overlay(alignment: .leading) {
                    Rectangle().fill(theme(.peach)).frame(width: 3)
                }
            }
            if tab == .session {
                if let failure = model.failure {
                    failureBox(failure, theme)
                }
            } else {
                queue(theme)
            }
            Spacer(minLength: 0)
            footer(theme)
        }
        .padding(20)
        .frame(minWidth: 560, minHeight: 520)
        .foregroundStyle(theme(.text))
        .background(theme(.base))
        .task(id: live ? model.form.workItem + "\n" + model.form.workspace : "") {
            guard live, tab == .session else { return }
            try? await Task.sleep(for: .milliseconds(300))
            if Task.isCancelled { return }
            await model.resolve(in: state)
        }
    }

    private func row<Content: View>(_ label: String, @ViewBuilder _ content: () -> Content) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(label)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(Theme(scheme)(.subtext))
                .frame(width: 84, alignment: .trailing)
            content().frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var workspacePicker: some View {
        Picker("", selection: $model.form.workspace) {
            ForEach(model.form.workspaces) { ws in
                Text("\(ws.name) — \(ws.detail)").tag(ws.root)
            }
        }
        .labelsHidden()
    }

    private func workItem(_ theme: Theme) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            TextField("Linear issue, PR link or text", text: $model.form.workItem)
                .textFieldStyle(.roundedBorder)
            if let card = model.card {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 6) {
                        Text(card.source)
                            .font(.system(size: 10, weight: .semibold))
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Capsule().fill(theme(.blue).opacity(0.16)))
                            .foregroundStyle(theme(.blue))
                        Text(card.title).font(.system(size: 13, weight: .semibold)).lineLimit(2)
                    }
                    Label(card.branch, systemImage: "arrow.triangle.branch")
                        .font(Metrics.mono)
                        .foregroundStyle(theme(.subtext))
                    if let existing = card.existing {
                        Label("existing worktree · \(existing)", systemImage: "folder")
                            .font(Metrics.mono)
                            .foregroundStyle(theme(.subtext))
                    }
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: Metrics.corner).fill(theme(.mantle)))
                .overlay(RoundedRectangle(cornerRadius: Metrics.corner).stroke(theme(.surface)))
            } else if let error = model.resolveError {
                Text(error).font(.system(size: 11)).foregroundStyle(theme(.red))
            }
        }
    }

    private func launchInput(_ theme: Theme) -> some View {
        TextEditor(text: $model.form.launchInput)
            .font(Metrics.mono)
            .frame(height: 84)
            .scrollContentBackground(.hidden)
            .padding(4)
            .background(RoundedRectangle(cornerRadius: Metrics.corner).fill(theme(.mantle)))
            .overlay(RoundedRectangle(cornerRadius: Metrics.corner).stroke(theme(.surface)))
    }

    private var harnessPicker: some View {
        Picker("", selection: $model.form.harness) {
            ForEach(model.form.harnesses, id: \.harness) { h in
                Text(h.name).tag(h.harness)
            }
        }
        .pickerStyle(.segmented)
        .labelsHidden()
        .fixedSize()
    }

    private var modelPicker: some View {
        choicePicker(selection: $model.form.model, choices: model.form.chosen?.models ?? [])
    }

    private var effortPicker: some View {
        choicePicker(selection: $model.form.effort, choices: model.form.chosen?.efforts ?? [])
    }

    private func choicePicker(selection: Binding<String>, choices: [String]) -> some View {
        let current = selection.wrappedValue
        let all = current.isEmpty || choices.contains(current) ? choices : choices + [current]
        return Picker("", selection: selection) {
            Text("default").tag("")
            ForEach(all, id: \.self) { Text($0).tag($0) }
        }
        .labelsHidden()
        .fixedSize()
    }

    private func failureBox(_ text: String, _ theme: Theme) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Label("Could not start the session", systemImage: "exclamationmark.octagon.fill")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(theme(.red))
            ScrollView {
                Text(text)
                    .font(Metrics.mono)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(maxHeight: 120)
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: Metrics.corner).fill(theme(.red).opacity(0.10)))
        .overlay(RoundedRectangle(cornerRadius: Metrics.corner).stroke(theme(.red).opacity(0.5)))
    }

    private func queue(_ theme: Theme) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("Queue").font(.system(size: 12, weight: .semibold))
                Spacer()
                Text("up to \(model.maxParallel) at once").font(.system(size: 11)).foregroundStyle(theme(.subtext))
            }
            if model.queue.isEmpty {
                Text("Nothing waiting.").font(.system(size: 12)).foregroundStyle(theme(.subtext))
            }
            ForEach(model.queue) { item in
                HStack(spacing: 8) {
                    Text(item.harness)
                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                        .foregroundStyle(theme(.subtext))
                    Text(item.ref).font(Metrics.mono)
                    Spacer()
                    Text(item.status)
                        .font(.system(size: 11))
                        .foregroundStyle(theme(item.status.hasPrefix("failed") ? .red : item.status == "starting" ? .blue : .subtext))
                }
                .padding(.vertical, 3)
            }
            if let note = model.launchNote {
                Text(note).font(.system(size: 11)).foregroundStyle(theme(.subtext))
            }
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: Metrics.corner).fill(theme(.mantle)))
    }

    private func footer(_ theme: Theme) -> some View {
        HStack {
            Text(tab == .session ? "You type the first prompt in the session's terminal." : "Each issue gets its own session.")
                .font(.system(size: 11))
                .foregroundStyle(theme(.subtext))
            Spacer()
            Button("Cancel", action: cancel)
                .keyboardShortcut(.cancelAction)
            if tab == .session {
                Button(model.starting ? "Starting…" : "Start") {
                    Task {
                        if let id = await model.start() { started(id) }
                    }
                }
                .keyboardShortcut(.return, modifiers: .command)
                .buttonStyle(.borderedProminent)
                .disabled(!model.form.canStart || model.starting)
            } else {
                Button("Queue") {
                    Task { await model.enqueue() }
                }
                .keyboardShortcut(.return, modifiers: .command)
                .buttonStyle(.borderedProminent)
            }
        }
    }
}

struct Notice: View {
    let text: String
    let tone: Tone
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        Text(text)
            .font(.system(size: 12))
            .padding(8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: Metrics.corner).fill(theme(tone).opacity(0.14)))
    }
}
#endif
