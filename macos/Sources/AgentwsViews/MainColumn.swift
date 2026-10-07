#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

struct MainColumn: View {
    let scene: WindowScene
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme
#if canImport(SwiftTerm)
    @Environment(\.agentwsTerminals) private var terminals
#endif

    @ViewBuilder
    private func terminal(_ session: Session, _ theme: Theme) -> some View {
#if canImport(SwiftTerm)
        if let terminals, !shown(session).isEmpty {
            TerminalArea(hub: terminals.hub, views: terminals.views, pane: shown(session))
                .overlay { popupShell(terminals) }
        } else {
            placeholder(session, theme)
        }
#else
        placeholder(session, theme)
#endif
    }

    private func shown(_ session: Session) -> String {
        scene.pane ?? session.pane
    }

#if canImport(SwiftTerm)
    @ViewBuilder
    private func popupShell(_ terminals: (hub: TerminalHub, views: TerminalViews)) -> some View {
        if let popup = scene.popup {
            let theme = Theme(scheme)
            VStack(spacing: 0) {
                HStack {
                    Text("Shell").font(.system(size: 12, weight: .semibold))
                    Spacer()
                    Button("Close", action: actions.closePopup).buttonStyle(.plain).font(.system(size: 12))
                }
                .padding(.horizontal, 10)
                .frame(height: 26)
                .background(theme(.mantle))
                TerminalArea(hub: terminals.hub, views: terminals.views, pane: popup)
            }
            .background(theme(.base))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).stroke(theme(.surface)))
            .padding(40)
        }
    }
#endif

    private func placeholder(_ session: Session, _ theme: Theme) -> some View {
        VStack(spacing: 8) {
            Image(systemName: "terminal").font(.system(size: 28)).foregroundStyle(theme(.grey))
            Text(session.pane.isEmpty ? "This session has no pane" : "Connecting to the terminal…")
                .font(.system(size: 13))
                .foregroundStyle(theme(.subtext))
            if !session.banner.isEmpty {
                Text(session.banner).font(Metrics.mono).foregroundStyle(theme(.subtext))
            }
        }
    }

    var body: some View {
        let theme = Theme(scheme)
        VStack(spacing: 0) {
            if let session = scene.session {
                HeaderStrip(header: Header(session: session, now: scene.now))
                Rectangle().fill(theme(.surface)).frame(height: 1)
                terminal(session, theme)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(theme(.base))
                Rectangle().fill(theme(.surface)).frame(height: 1)
                HStack(spacing: 14) {
                    Text(shown(session).isEmpty ? "no pane" : scene.view.rawValue + " · pane " + shown(session))
                    if let error = scene.paneError {
                        Text(error).foregroundStyle(theme(.red)).lineLimit(1)
                    }
                    Text("⌘1–9 jump")
                    Text("⌃Space next waiting")
                    Text("⌘[ last")
                    Text("⌘T shell")
                    Text("⌘E nvim")
                    Text("⌘K filter")
                    Text("⌥⌘I inspector")
                    Spacer()
                }
                .font(.system(size: 11))
                .foregroundStyle(theme(.subtext))
                .padding(.horizontal, Metrics.gutter)
                .frame(height: 26)
                .background(theme(.mantle))
            } else {
                VStack(spacing: 8) {
                    Image(systemName: "rectangle.stack").font(.system(size: 28)).foregroundStyle(theme(.grey))
                    Text(scene.state == nil ? "Waiting for the daemon" : "Select a session")
                        .font(.system(size: 13))
                        .foregroundStyle(theme(.subtext))
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }
}

struct HeaderStrip: View {
    let header: Header
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        HStack(spacing: 8) {
            HStack(spacing: 6) {
                GlyphView(style: header.style, size: 9)
                Text(header.chip).font(.system(size: 12, weight: .semibold))
            }
            .padding(.horizontal, 9)
            .padding(.vertical, 4)
            .background(Capsule().fill(theme(header.style.tone).opacity(0.16)))
            ForEach(header.prs, id: \.self) { pr in
                Text(pr)
                    .font(.system(size: 12, design: .monospaced))
                    .padding(.horizontal, 8)
                    .padding(.vertical, 3)
                    .background(Capsule().stroke(theme(.surface)))
            }
            Spacer()
            if !header.model.isEmpty {
                Text(header.model).font(.system(size: 12)).foregroundStyle(theme(.subtext))
            }
            if let context = header.context {
                Text(context).font(.system(size: 12)).foregroundStyle(theme(.subtext))
            }
        }
        .padding(.horizontal, Metrics.gutter)
        .frame(height: 38)
    }
}

struct InspectorView: View {
    let session: Session?
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        VStack(alignment: .leading, spacing: 10) {
            Text("Pull requests").font(.system(size: 12, weight: .semibold)).foregroundStyle(theme(.subtext))
            let board = session?.board ?? []
            if board.isEmpty {
                Text("No pull requests yet").font(.system(size: 12)).foregroundStyle(theme(.grey))
            }
            ForEach(board, id: \.number) { pr in
                PRCard(pr: pr)
            }
            Spacer()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(Metrics.gutter)
    }
}

struct PRCard: View {
    let pr: BoardPR
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        let checkTone: Tone = switch pr.checks {
        case "passing": .green
        case "failing": .red
        case "pending": .peach
        default: .grey
        }
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Text("#\(pr.number)").font(.system(size: 12, weight: .bold, design: .monospaced))
                Text(pr.title).font(.system(size: 12)).lineLimit(1)
            }
            Text(pr.branch).font(Metrics.mono).foregroundStyle(theme(.subtext))
            HStack(spacing: 6) {
                Text([Sidebar.checkMark(pr.checks), pr.checks.isEmpty ? "no checks" : pr.checks].filter { !$0.isEmpty }.joined(separator: " "))
                    .foregroundStyle(theme(checkTone))
                if pr.unresolvedThreads > 0 { Text("\(pr.unresolvedThreads) unresolved") }
                if !pr.reviewDecision.isEmpty { Text(pr.reviewDecision.lowercased().replacingOccurrences(of: "_", with: " ")) }
            }
            .font(.system(size: 11))
            ForEach(pr.failingChecks, id: \.name) { check in
                Text("✗ " + check.name).font(.system(size: 11)).foregroundStyle(theme(.red))
            }
            if pr.readyToMerge {
                Text("Ready to merge").font(.system(size: 11, weight: .semibold)).foregroundStyle(theme(.green))
            } else {
                ForEach(pr.blockers, id: \.self) { blocker in
                    Text("· " + blocker).font(.system(size: 11)).foregroundStyle(theme(.subtext))
                }
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 8).fill(theme(.base)))
    }
}
#endif
