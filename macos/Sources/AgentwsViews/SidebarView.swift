#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

struct GlyphView: View {
    let style: StateStyle
    var size: CGFloat = 10
    @Environment(\.colorScheme) private var scheme
    @Environment(\.agentwsAnimates) private var animates

    var body: some View {
        let color = Theme(scheme)(style.tone)
        Group {
            switch style.glyph {
            case .filledDiamond:
                Rectangle().fill(color).frame(width: size * 0.72, height: size * 0.72).rotationEffect(.degrees(45))
            case .filledDot:
                Circle().fill(color).overlay {
                    if style.symbol == "✓" {
                        Image(systemName: "checkmark")
                            .font(.system(size: size * 0.55, weight: .black))
                            .foregroundStyle(Theme(scheme)(.base))
                    }
                }
            case .hollowRing:
                Circle().strokeBorder(color, lineWidth: 2.5)
            case .spinningRing:
                if animates {
                    TimelineView(.animation) { context in
                        ring(color).rotationEffect(.degrees(context.date.timeIntervalSinceReferenceDate * 360))
                    }
                } else {
                    ring(color)
                }
            }
        }
        .frame(width: size, height: size)
        .accessibilityLabel(style.label)
    }

    private func ring(_ color: Color) -> some View {
        Circle().trim(from: 0, to: 0.72).stroke(color, style: StrokeStyle(lineWidth: 2.5, lineCap: .round))
    }
}

struct SidebarView: View {
    let scene: WindowScene
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme
    @FocusState private var filterFocused: Bool

    var body: some View {
        let theme = Theme(scheme)
        let sidebar = scene.sidebar
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass").foregroundStyle(theme(.grey))
                TextField("Filter", text: Binding(get: { scene.filter }, set: actions.filter))
                    .textFieldStyle(.plain)
                    .focused($filterFocused)
                    .onChange(of: scene.focusFilter) { filterFocused = true }
                    .font(.system(size: 12))
                Text("⌘K").font(.system(size: 10)).foregroundStyle(theme(.grey))
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 6)
            .background(RoundedRectangle(cornerRadius: Metrics.corner).fill(theme(.crust)))
            .padding(Metrics.gutter)
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 2) {
                    ForEach(sidebar?.rows ?? []) { row in
                        SidebarRowView(row: row, selected: row.id == scene.selected)
                            .onTapGesture { actions.select(row.id) }
                    }
                    if let ended = sidebar?.ended, !ended.isEmpty {
                        Button(action: actions.toggleEnded) {
                            HStack(spacing: 4) {
                                Image(systemName: scene.endedExpanded ? "chevron.down" : "chevron.right")
                                Text("Ended · \(ended.count)")
                            }
                            .font(.system(size: 11, weight: .semibold))
                            .foregroundStyle(theme(.subtext))
                        }
                        .buttonStyle(.plain)
                        .padding(.horizontal, Metrics.gutter)
                        .padding(.top, 10)
                        if scene.endedExpanded {
                            ForEach(ended) { row in
                                SidebarRowView(row: row, selected: row.id == scene.selected)
                                    .opacity(0.7)
                                    .onTapGesture { actions.select(row.id) }
                            }
                        }
                    }
                }
                .padding(.horizontal, 6)
            }
            Rectangle().fill(theme(.surface)).frame(height: 1)
            Text(sidebar?.footer ?? "")
                .font(.system(size: 11))
                .foregroundStyle(theme(.subtext))
                .padding(Metrics.gutter)
        }
    }
}

struct SidebarRowView: View {
    let row: SidebarRow
    let selected: Bool
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 7) {
                GlyphView(style: row.style)
                Text(row.name)
                    .font(.system(size: 13, weight: row.bold ? .bold : .regular))
                    .lineLimit(1)
                Text(row.badge)
                    .font(.system(size: 9, weight: .bold, design: .monospaced))
                    .padding(.horizontal, 4)
                    .padding(.vertical, 1)
                    .background(RoundedRectangle(cornerRadius: 3).fill(theme(.surface)))
                    .foregroundStyle(theme(.subtext))
                if row.muted {
                    Image(systemName: "bell.slash").font(.system(size: 10)).foregroundStyle(theme(.grey))
                }
                Spacer(minLength: 4)
                if let shortcut = row.shortcut {
                    Text(shortcut).font(.system(size: 10)).foregroundStyle(theme(.grey))
                }
            }
            VStack(alignment: .leading, spacing: 1) {
                if !row.where.isEmpty {
                    Text(row.where).font(Metrics.mono).foregroundStyle(theme(.subtext)).lineLimit(1)
                }
                if !row.detail.isEmpty {
                    Text(row.detail).font(.system(size: 11)).foregroundStyle(theme(.subtext)).lineLimit(1)
                }
                ForEach(row.worktrees, id: \.self) { line in
                    Text("└ " + line).font(Metrics.mono).foregroundStyle(theme(.grey)).lineLimit(1)
                }
            }
            .padding(.leading, 17)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: Metrics.corner).fill(selected ? theme(.surface).opacity(0.7) : .clear))
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityValue(row.style.label)
    }
}
#endif
