#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

public enum DiskTab: String, CaseIterable, Sendable {
    case worktrees = "Worktrees"
    case ports = "Ports"
    case recent = "Recently cleaned"
}

public struct DiskScene {
    public var disk: DiskView?
    public var state: ViewState?
    public var tab: DiskTab
    public var selected: String?
    public var now: Date
    public var message: String?

    public init(disk: DiskView?, state: ViewState?, tab: DiskTab = .worktrees, selected: String? = nil, now: Date = Seed.now, message: String? = nil) {
        self.disk = disk
        self.state = state
        self.tab = tab
        self.selected = selected
        self.now = now
        self.message = message
    }

    public static func seeded(tab: DiskTab = .worktrees, measuring: Bool = false) -> DiskScene {
        DiskScene(disk: Seed.disk(measuring: measuring), state: Seed.window, tab: tab, selected: "w8")
    }

    var rows: [DiskTableRow] { disk.map { DiskTable.rows($0, state: state) } ?? [] }
}

@MainActor
public struct DiskWindowActions {
    public var tab: (DiskTab) -> Void = { _ in }
    public var select: (String) -> Void = { _ in }
    public var perform: (DiskAction) -> Void = { _ in }

    public init() {}
}

public struct DiskWindow: View {
    let scene: DiskScene
    let actions: DiskWindowActions
    @Environment(\.colorScheme) private var scheme

    public init(scene: DiskScene, actions: DiskWindowActions = DiskWindowActions()) {
        self.scene = scene
        self.actions = actions
    }

    public var body: some View {
        let theme = Theme(scheme)
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 2) {
                ForEach(DiskTab.allCases, id: \.self) { tab in
                    Button { actions.tab(tab) } label: {
                        Text(tab.rawValue)
                            .font(.system(size: 12, weight: .medium))
                            .padding(.horizontal, 12).padding(.vertical, 4)
                            .background(RoundedRectangle(cornerRadius: 5).fill(scene.tab == tab ? theme(.base) : .clear))
                            .foregroundStyle(scene.tab == tab ? theme(.text) : theme(.subtext))
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(3)
            .background(RoundedRectangle(cornerRadius: 7).fill(theme(.crust)))
            .padding(Metrics.gutter)
            if let disk = scene.disk {
                DiskTilesView(tiles: DiskTiles(disk))
                    .padding(.horizontal, Metrics.gutter)
                    .padding(.bottom, Metrics.gutter)
            }
            Rectangle().fill(theme(.surface)).frame(height: 1)
            switch scene.tab {
            case .worktrees: worktrees(theme)
            case .ports: ports(theme)
            case .recent: recent(theme)
            }
            if let message = scene.message {
                Text(message)
                    .font(.system(size: 12))
                    .foregroundStyle(theme(.subtext))
                    .padding(Metrics.gutter)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .foregroundStyle(theme(.text))
        .background(theme(.base))
    }

    @ViewBuilder
    private func worktrees(_ theme: Theme) -> some View {
        let rows = scene.rows
        if scene.disk == nil {
            placeholder("Measuring worktrees…", theme)
        } else if rows.isEmpty {
            placeholder("No worktrees.", theme)
        } else {
            DiskHeaderRow(columns: ["Worktree", "Session", "PR", "State", "Size", "Ports", "Cleanup"])
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(rows) { row in
                        DiskRowView(row: row, selected: row.id == scene.selected)
                            .contentShape(Rectangle())
                            .onTapGesture { actions.select(row.id) }
                    }
                }
            }
            Spacer(minLength: 0)
            if let row = rows.first(where: { $0.id == scene.selected }) {
                Rectangle().fill(theme(.surface)).frame(height: 1)
                DiskActionBar(row: row, perform: actions.perform)
            }
        }
    }

    @ViewBuilder
    private func ports(_ theme: Theme) -> some View {
        let rows = DiskPorts.rows(scene.state)
        if rows.isEmpty {
            placeholder("No dev servers are listening in a worktree.", theme)
        } else {
            DiskHeaderRow(columns: ["Port", "Command", "Worktree", "Session", ""])
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(rows) { p in
                        HStack(spacing: 8) {
                            Text(":\(p.port)").font(Metrics.mono).frame(maxWidth: .infinity, alignment: .leading)
                            Text(p.command).font(Metrics.mono).frame(maxWidth: .infinity, alignment: .leading)
                            Text(p.worktree).frame(maxWidth: .infinity, alignment: .leading)
                            Text(p.session).foregroundStyle(theme(.subtext)).frame(maxWidth: .infinity, alignment: .leading)
                            Button("Kill…") { actions.perform(DiskActions.kill(p)) }
                            .frame(maxWidth: .infinity, alignment: .trailing)
                        }
                        .font(.system(size: 12))
                        .lineLimit(1)
                        .padding(.horizontal, Metrics.gutter).padding(.vertical, 6)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func recent(_ theme: Theme) -> some View {
        let rows = scene.disk.map { DiskRecent.rows($0, now: scene.now) } ?? []
        if rows.isEmpty {
            placeholder("Nothing cleaned yet.", theme)
        } else {
            DiskHeaderRow(columns: ["Worktree", "Branch", "When", "Outcome"])
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(rows) { r in
                        HStack(spacing: 8) {
                            Text(r.worktree).frame(maxWidth: .infinity, alignment: .leading)
                            Text(r.branch).font(Metrics.mono).frame(maxWidth: .infinity, alignment: .leading)
                            Text(r.when).foregroundStyle(theme(.subtext)).frame(maxWidth: .infinity, alignment: .leading)
                            Text(r.outcome).frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .font(.system(size: 12))
                        .lineLimit(1)
                        .padding(.horizontal, Metrics.gutter).padding(.vertical, 6)
                    }
                }
            }
        }
    }

    private func placeholder(_ text: String, _ theme: Theme) -> some View {
        Text(text)
            .font(.system(size: 13))
            .foregroundStyle(theme(.subtext))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

struct DiskTilesView: View {
    let tiles: DiskTiles
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        HStack(spacing: Metrics.gutter) {
            tile("Volume", tiles.free)
            tile("Worktrees", tiles.worktrees)
            tile("Reclaimable", tiles.reclaimable, tone: .green)
            if let deps = tiles.depsStore { tile("Shared deps store", deps) }
            tile("Auto-cleanup", tiles.schedule)
        }
    }

    private func tile(_ title: String, _ value: String, tone: Tone = .text) -> some View {
        let theme = Theme(scheme)
        return VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.system(size: 11, weight: .medium)).foregroundStyle(theme(.subtext))
            Text(value).font(.system(size: 18, weight: .semibold)).foregroundStyle(theme(tone))
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(RoundedRectangle(cornerRadius: Metrics.corner).fill(theme(.mantle)))
    }
}

struct DiskHeaderRow: View {
    let columns: [String]
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        HStack(spacing: 8) {
            ForEach(Array(columns.enumerated()), id: \.offset) { _, title in
                Text(title).frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .font(.system(size: 11, weight: .medium))
        .foregroundStyle(theme(.subtext))
        .padding(.horizontal, Metrics.gutter).padding(.vertical, 6)
        .background(theme(.mantle))
    }
}

struct DiskRowView: View {
    let row: DiskTableRow
    let selected: Bool
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        HStack(spacing: 8) {
            Text(row.worktree).fontWeight(.medium).frame(maxWidth: .infinity, alignment: .leading)
            Text(row.session).foregroundStyle(theme(.subtext)).frame(maxWidth: .infinity, alignment: .leading)
            Text(row.pr).font(Metrics.mono).frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 5) {
                Circle().fill(theme(row.tone)).frame(width: 7, height: 7)
                Text(row.status).foregroundStyle(theme(row.tone))
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Text(row.size).font(Metrics.mono).frame(maxWidth: .infinity, alignment: .leading)
            Text(row.ports).font(Metrics.mono).frame(maxWidth: .infinity, alignment: .leading)
            Text(row.plan).foregroundStyle(theme(.subtext)).frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(size: 12))
        .lineLimit(1)
        .padding(.horizontal, Metrics.gutter).padding(.vertical, 6)
        .background(selected ? theme(.blue).opacity(0.14) : .clear)
    }
}

struct DiskActionBar: View {
    let row: DiskTableRow
    let perform: (DiskAction) -> Void
    @State private var hovered: DiskAction?
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        let actions = DiskActions.for(row)
        let shown = hovered ?? actions.last(where: { $0.kind.destructive }) ?? actions.first
        HStack(spacing: 8) {
            Text(shown?.outcome ?? "")
                .font(.system(size: 12))
                .foregroundStyle(theme(.subtext))
                .lineLimit(2)
                .frame(maxWidth: .infinity, alignment: .leading)
            ForEach(actions) { action in
                Button(action.title) { perform(action) }
                    .foregroundStyle(action.kind.destructive ? theme(.red) : theme(.text))
                    .onHover { inside in hovered = inside ? action : nil }
            }
        }
        .padding(Metrics.gutter)
        .background(theme(.mantle))
    }
}
#endif
