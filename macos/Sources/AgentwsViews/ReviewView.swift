#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI
#if canImport(AppKit)
import AppKit
#endif

struct RailView: View {
    let scene: WindowScene
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        VStack(spacing: 8) {
            Button(action: actions.toggleSidebar) {
                Image(systemName: "sidebar.left").foregroundStyle(theme(.subtext))
            }
            .buttonStyle(.plain)
            .help("Show the sidebar (⌃⌘S)")
            ForEach(Array((scene.sidebar?.rows ?? []).enumerated()), id: \.element.id) { index, row in
                Button { actions.select(row.id) } label: {
                    ZStack(alignment: .bottomTrailing) {
                        Text(index < 9 ? "\(index + 1)" : "·")
                            .font(.system(size: 12, weight: .semibold, design: .rounded))
                            .frame(width: 30, height: 30)
                            .background(RoundedRectangle(cornerRadius: 7).fill(row.id == scene.selected ? theme(.surface) : theme(.crust)))
                        GlyphView(style: row.style, size: 9).offset(x: 2, y: 2)
                    }
                }
                .buttonStyle(.plain)
                .help(row.name)
            }
            Spacer()
        }
        .padding(.vertical, Metrics.gutter)
        .frame(maxWidth: .infinity)
    }
}

struct ReviewView: View {
    let review: ReviewScreen
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme
    @FocusState private var diffFocused: Bool

    var body: some View {
        let theme = Theme(scheme)
        VStack(spacing: 0) {
            ReviewBar(review: review, actions: actions)
            Rectangle().fill(theme(.surface)).frame(height: 1)
            if let error = review.error {
                ReviewErrorView(message: error, dismiss: actions.dismissError)
            }
            HStack(spacing: 0) {
                FileTreeView(review: review, actions: actions)
                    .frame(width: Metrics.treeWidth)
                    .background(theme(.mantle))
                Rectangle().fill(theme(.surface)).frame(width: 1)
                if let selected = review.selectedFile {
                    DiffView(review: review, selected: selected, actions: actions)
                        .focusable()
                        .focused($diffFocused)
                        .focusEffectDisabled()
                        .onKeyPress(characters: CharacterSet(charactersIn: "vVcC[]")) { press in
                            key(press.characters, selected: selected)
                        }
                } else {
                    VStack(spacing: 8) {
                        Image(systemName: "checkmark.circle").font(.system(size: 26)).foregroundStyle(theme(.grey))
                        Text(review.result == nil ? "Loading the review" : "No changes in this scope")
                            .font(.system(size: 13))
                            .foregroundStyle(theme(.subtext))
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
        }
        .background(theme(.base))
        .onAppear { diffFocused = true }
    }

    private func key(_ characters: String, selected: SelectedFile) -> KeyPress.Result {
        switch characters {
        case "v", "V":
            actions.toggleViewed(selected.key)
        case "c", "C":
            guard let first = selected.file.hunks.first?.lines.first(where: { $0.kind != .deleted }) ?? selected.file.hunks.first?.lines.first else {
                return .ignored
            }
            actions.startComment(selected.key, first)
        case "[":
            actions.stepScope(-1)
        case "]":
            actions.stepScope(1)
        default:
            return .ignored
        }
        return .handled
    }
}

struct ReviewBar: View {
    let review: ReviewScreen
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        HStack(spacing: 10) {
            HStack(spacing: 2) {
                ForEach(ReviewScope.allCases, id: \.self) { scope in
                    Button { actions.setScope(scope) } label: {
                        Text(scope.title)
                            .font(.system(size: 12, weight: scope == review.scope ? .semibold : .regular))
                            .padding(.horizontal, 10)
                            .padding(.vertical, 4)
                            .background(RoundedRectangle(cornerRadius: 5).fill(scope == review.scope ? theme(.base) : .clear))
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(2)
            .background(RoundedRectangle(cornerRadius: 7).fill(theme(.crust)))
            .help("[ and ] step the scope")
            Menu {
                ForEach(review.worktreeMenu) { choice in
                    Button(choice.title) { actions.setWorktree(choice.id) }
                }
            } label: {
                Text(review.worktreeMenu.first { $0.id == review.worktree }?.title ?? "All").font(.system(size: 12))
            }
            .menuStyle(.borderlessButton)
            .fixedSize()
            Text("\(review.fileCount) files").font(.system(size: 12)).foregroundStyle(theme(.subtext))
            Spacer()
            Picker("", selection: Binding(get: { review.layout }, set: actions.setLayout)) {
                Text("Unified").tag(DiffLayout.unified)
                Text("Split").tag(DiffLayout.split)
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .fixedSize()
            Button(action: actions.openInNvim) {
                Label("nvim", systemImage: "chevron.left.forwardslash.chevron.right").font(.system(size: 12))
            }
            .buttonStyle(.plain)
            .help("Open in nvim (⌘E)")
        }
        .padding(.horizontal, Metrics.gutter)
        .frame(height: 40)
        .background(theme(.mantle))
    }
}

struct ReviewErrorView: View {
    let message: String
    let dismiss: () -> Void
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        HStack(alignment: .top, spacing: 10) {
            Rectangle().fill(theme(.red)).frame(width: 4)
            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(theme(.red))
            Text(message)
                .font(Metrics.mono)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.vertical, 8)
            Button(action: dismiss) { Image(systemName: "xmark").foregroundStyle(theme(.subtext)) }
                .buttonStyle(.plain)
                .padding(.top, 8)
        }
        .padding(.trailing, Metrics.gutter)
        .fixedSize(horizontal: false, vertical: true)
        .background(theme(.red).opacity(0.14))
    }
}

struct FileTreeView: View {
    let review: ReviewScreen
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        let selected = review.selectedFile?.key
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 2) {
                ForEach(review.tree) { group in
                    Text(group.title)
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(theme(.subtext))
                        .padding(.top, 8)
                        .padding(.horizontal, 10)
                    if !group.error.isEmpty {
                        Text(group.error).font(.system(size: 11)).foregroundStyle(theme(.red)).padding(.horizontal, 10)
                    }
                    ForEach(group.files) { file in
                        Button { actions.selectFile(file.key) } label: {
                            HStack(spacing: 6) {
                                Image(systemName: file.viewed ? "checkmark.square.fill" : "square")
                                    .foregroundStyle(file.viewed ? theme(.green) : theme(.grey))
                                    .font(.system(size: 11))
                                Text(file.status).font(.system(size: 10, weight: .bold, design: .monospaced))
                                    .foregroundStyle(file.status == "A" ? theme(.green) : file.status == "D" ? theme(.red) : theme(.peach))
                                Text((file.path as NSString).lastPathComponent)
                                    .font(.system(size: 12))
                                    .lineLimit(1)
                                    .foregroundStyle(file.viewed ? theme(.subtext) : theme(.text))
                                Spacer(minLength: 4)
                                Text("+\(file.added)").font(.system(size: 10, design: .monospaced)).foregroundStyle(theme(.green))
                                Text("−\(file.deleted)").font(.system(size: 10, design: .monospaced)).foregroundStyle(theme(.red))
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 4)
                            .background(RoundedRectangle(cornerRadius: 5).fill(file.key == selected ? theme(.surface) : .clear))
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .help(file.path)
                    }
                }
            }
            .padding(.vertical, 6)
        }
    }
}

struct DiffView: View {
    let review: ReviewScreen
    let selected: SelectedFile
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        let viewed = review.isViewed(selected.key, blob: selected.file.blob)
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Text(selected.worktree.title).font(.system(size: 11)).foregroundStyle(theme(.subtext))
                Text(selected.file.path).font(.system(size: 12, weight: .semibold, design: .monospaced)).lineLimit(1)
                Spacer()
                Button { actions.toggleViewed(selected.key) } label: {
                    Label("Viewed", systemImage: viewed ? "checkmark.square.fill" : "square")
                        .font(.system(size: 12))
                        .foregroundStyle(viewed ? theme(.green) : theme(.subtext))
                }
                .buttonStyle(.plain)
                .help("V toggles viewed")
            }
            .padding(.horizontal, Metrics.gutter)
            .frame(height: 32)
            .background(theme(.crust))
            ScrollView([.vertical, .horizontal]) {
                LazyVStack(alignment: .leading, spacing: 0) {
                    if selected.file.binary {
                        Text("Binary file").font(.system(size: 12)).foregroundStyle(theme(.subtext)).padding(Metrics.gutter)
                    } else if selected.file.hunks.isEmpty {
                        Text("No text changes").font(.system(size: 12)).foregroundStyle(theme(.subtext)).padding(Metrics.gutter)
                    }
                    ForEach(Array(selected.file.hunks.enumerated()), id: \.offset) { index, hunk in
                        HunkHeader(header: hunk.header, stage: { actions.hunk(selected.key, index, .stage) },
                                   revert: { actions.hunk(selected.key, index, .revert) })
                        if review.layout == .split {
                            ForEach(Array(SplitRow.rows(hunk).enumerated()), id: \.offset) { _, row in
                                SplitLineRow(row: row, selected: selected, review: review, actions: actions)
                                composer(after: row.right ?? row.left)
                            }
                        } else {
                            ForEach(Array(hunk.lines.enumerated()), id: \.offset) { _, line in
                                UnifiedLineRow(line: line, selected: selected, review: review, actions: actions)
                                composer(after: line)
                            }
                        }
                    }
                }
                .frame(minWidth: 600, alignment: .leading)
            }
        }
    }

    @ViewBuilder
    private func composer(after line: DiffLine?) -> some View {
        if let composer = review.composer, composer.key == selected.key, let line, composer.lines.last == line {
            CommentComposer(composer: composer, actions: actions)
        }
    }
}

struct HunkHeader: View {
    let header: String
    let stage: () -> Void
    let revert: () -> Void
    @State private var confirming = false
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        HStack(spacing: 10) {
            Text(header).font(Metrics.mono).foregroundStyle(theme(.subtext)).lineLimit(1)
            Spacer(minLength: 20)
            Button("Stage hunk", action: stage).buttonStyle(.plain).font(.system(size: 11, weight: .medium)).foregroundStyle(theme(.blue))
            Button("Revert…") { confirming = true }.buttonStyle(.plain).font(.system(size: 11, weight: .medium)).foregroundStyle(theme(.red))
        }
        .padding(.horizontal, Metrics.gutter)
        .frame(height: 26)
        .background(theme(.blue).opacity(0.08))
        .confirmationDialog("Revert this hunk?", isPresented: $confirming) {
            Button("Revert", role: .destructive, action: revert)
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The change is removed from the working tree. This cannot be undone from agentws.")
        }
    }
}

struct LineNumber: View {
    let number: Int
    let line: DiffLine
    let selected: SelectedFile
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        Text(number > 0 ? "\(number)" : "")
            .font(Metrics.mono)
            .foregroundStyle(Theme(scheme)(.grey))
            .frame(width: 40, alignment: .trailing)
            .contentShape(Rectangle())
            .onTapGesture {
                if Self.shift { actions.extendComment(line) } else { actions.startComment(selected.key, line) }
            }
            .help("Click to comment, shift-click for a range (C)")
    }

    static var shift: Bool {
        #if canImport(AppKit)
        NSEvent.modifierFlags.contains(.shift)
        #else
        false
        #endif
    }
}

struct CodeText: View {
    let line: DiffLine
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        Highlight.segments(line).reduce(Text(verbatim: "")) { text, segment in
            text + Text(verbatim: segment.text).foregroundStyle(theme.token(segment.cls))
        }
        .font(Metrics.code)
        .lineLimit(1)
        .fixedSize()
    }
}

func lineTone(_ kind: LineKind) -> Tone? {
    switch kind {
    case .added: .green
    case .deleted: .red
    case .context: nil
    }
}

func lineSign(_ kind: LineKind) -> String {
    switch kind {
    case .added: "+"
    case .deleted: "−"
    case .context: " "
    }
}

struct UnifiedLineRow: View {
    let line: DiffLine
    let selected: SelectedFile
    let review: ReviewScreen
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        let tone = lineTone(line.kind)
        let picked = review.composer?.key == selected.key && review.composer?.lines.contains(line) == true
        HStack(spacing: 0) {
            LineNumber(number: line.old, line: line, selected: selected, actions: actions)
            LineNumber(number: line.new, line: line, selected: selected, actions: actions)
            Text(lineSign(line.kind))
                .font(Metrics.code)
                .foregroundStyle(tone.map { theme($0) } ?? theme(.grey))
                .frame(width: 22)
            CodeText(line: line)
            Spacer(minLength: 0)
        }
        .frame(height: 19)
        .background(tone.map { theme($0).opacity(0.13) } ?? .clear)
        .background(picked ? theme(.blue).opacity(0.18) : .clear)
    }
}

struct SplitLineRow: View {
    let row: SplitRow
    let selected: SelectedFile
    let review: ReviewScreen
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        HStack(spacing: 0) {
            half(row.left, number: \.old, theme: theme)
            Rectangle().fill(theme(.surface)).frame(width: 1)
            half(row.right, number: \.new, theme: theme)
        }
        .frame(height: 19)
    }

    private func half(_ line: DiffLine?, number: KeyPath<DiffLine, Int>, theme: Theme) -> some View {
        let tone = line.flatMap { $0.kind == .context ? nil : lineTone($0.kind) }
        return HStack(spacing: 0) {
            if let line {
                LineNumber(number: line[keyPath: number], line: line, selected: selected, actions: actions)
                Text(lineSign(line.kind)).font(Metrics.code).foregroundStyle(tone.map { theme($0) } ?? theme(.grey)).frame(width: 18)
                CodeText(line: line)
            }
            Spacer(minLength: 0)
        }
        .frame(width: 520, alignment: .leading)
        .clipped()
        .background(tone.map { theme($0).opacity(0.13) } ?? (line == nil ? theme(.crust) : .clear))
    }
}

struct CommentComposer: View {
    let composer: Composer
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme
    @FocusState private var focused: Bool

    var body: some View {
        let theme = Theme(scheme)
        let anchor = CommentAnchor(lines: composer.lines)
        VStack(alignment: .leading, spacing: 6) {
            Text(anchor.map { $0.start == $0.end ? "Line \($0.start)" : "Lines \($0.start)–\($0.end)" } ?? "Comment")
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(theme(.subtext))
            TextEditor(text: Binding(get: { composer.text }, set: actions.editComment))
                .font(.system(size: 12))
                .scrollContentBackground(.hidden)
                .focused($focused)
                .frame(height: 56)
                .padding(4)
                .background(RoundedRectangle(cornerRadius: 5).fill(theme(.base)))
                .overlay(RoundedRectangle(cornerRadius: 5).stroke(theme(.surface)))
            HStack {
                Spacer()
                Button("Cancel", action: actions.cancelComment).keyboardShortcut(.cancelAction)
                Button("Comment", action: actions.submitComment)
                    .keyboardShortcut(.return, modifiers: .command)
                    .disabled(composer.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            .font(.system(size: 12))
        }
        .padding(10)
        .frame(width: 560, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 8).fill(theme(.mantle)))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(theme(.blue).opacity(0.5)))
        .padding(.leading, 104)
        .padding(.vertical, 6)
        .onAppear { focused = true }
    }
}

struct DraftPanel: View {
    let review: ReviewScreen
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        let rows = review.draftRows
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("Draft review").font(.system(size: 13, weight: .semibold))
                Spacer()
                Text("\(rows.count)").font(.system(size: 11, weight: .semibold)).foregroundStyle(theme(.subtext))
            }
            if rows.isEmpty {
                Text("Click a line number or press C to comment.").font(.system(size: 12)).foregroundStyle(theme(.grey))
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(rows) { row in
                        VStack(alignment: .leading, spacing: 3) {
                            Text(row.label).font(Metrics.mono).foregroundStyle(theme(.subtext)).lineLimit(1)
                            Text(row.body).font(.system(size: 12)).fixedSize(horizontal: false, vertical: true)
                        }
                        .padding(8)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(RoundedRectangle(cornerRadius: 6).fill(theme(.base)))
                    }
                }
            }
            Text("Overall note").font(.system(size: 11, weight: .semibold)).foregroundStyle(theme(.subtext))
            TextEditor(text: Binding(get: { review.note }, set: actions.editNote))
                .font(.system(size: 12))
                .scrollContentBackground(.hidden)
                .frame(height: 72)
                .padding(4)
                .background(RoundedRectangle(cornerRadius: 5).fill(theme(.base)))
                .overlay(RoundedRectangle(cornerRadius: 5).stroke(theme(.surface)))
            if let draft = review.draft, draft.status == "sent" || draft.status == "queued" {
                let title: String = draft.status == "sent" ? "Sent to the session" : "Queued until the session stops"
                Label(title, systemImage: "paperplane")
                    .font(.system(size: 12))
                    .foregroundStyle(theme(.green))
            } else if let notice = review.queueNotice, review.canSend {
                Text(notice).font(.system(size: 11)).foregroundStyle(theme(.peach)).fixedSize(horizontal: false, vertical: true)
            }
            Button(action: actions.sendReview) {
                Text("Send review to session  ⇧⌘↩")
                    .font(.system(size: 12, weight: .semibold))
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 6)
                    .background(RoundedRectangle(cornerRadius: 6).fill(review.canSend ? theme(.blue) : theme(.surface)))
                    .foregroundStyle(review.canSend ? theme(.base) : theme(.grey))
            }
            .buttonStyle(.plain)
            .disabled(!review.canSend)
        }
        .padding(Metrics.gutter)
    }
}
#endif
