#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

public struct WindowScene {
    public var state: ViewState?
    public var connection: ConnectionStatus
    public var selected: String?
    public var filter: String
    public var inspector: Bool
    public var endedExpanded: Bool
    public var server: String
    public var now: Date
    public var focusFilter: Int
    public var review: ReviewScreen?

    public init(
        state: ViewState?, connection: ConnectionStatus, selected: String? = nil, filter: String = "",
        inspector: Bool = true, endedExpanded: Bool = false, server: String = "This Mac", now: Date = Seed.now, focusFilter: Int = 0,
        review: ReviewScreen? = nil
    ) {
        self.state = state
        self.connection = connection
        self.selected = selected
        self.filter = filter
        self.inspector = inspector
        self.endedExpanded = endedExpanded
        self.server = server
        self.now = now
        self.focusFilter = focusFilter
        self.review = review
    }

    public static func seeded(selected: String = "s1", inspector: Bool = true) -> WindowScene {
        WindowScene(state: Seed.window, connection: .live, selected: selected, inspector: inspector)
    }

    public static func seededReview(layout: DiffLayout = .unified) -> WindowScene {
        WindowScene(state: Seed.window, connection: .live, selected: "s2", review: Seed.review(layout: layout))
    }

    var sidebar: Sidebar? { state.map { Sidebar(state: $0, filter: filter) } }

    var session: Session? { state?.sessions.first { $0.id == selected } }
}

@MainActor
public struct WindowActions {
    public var select: (String) -> Void = { _ in }
    public var filter: (String) -> Void = { _ in }
    public var toggleInspector: () -> Void = {}
    public var toggleEnded: () -> Void = {}
    public var reconnect: () -> Void = {}
    public var toggleReview: () -> Void = {}
    public var toggleSidebar: () -> Void = {}
    public var setScope: (ReviewScope) -> Void = { _ in }
    public var stepScope: (Int) -> Void = { _ in }
    public var setWorktree: (String) -> Void = { _ in }
    public var setLayout: (DiffLayout) -> Void = { _ in }
    public var selectFile: (FileKey) -> Void = { _ in }
    public var toggleViewed: (FileKey) -> Void = { _ in }
    public var startComment: (FileKey, DiffLine) -> Void = { _, _ in }
    public var extendComment: (DiffLine) -> Void = { _ in }
    public var editComment: (String) -> Void = { _ in }
    public var submitComment: () -> Void = {}
    public var cancelComment: () -> Void = {}
    public var editNote: (String) -> Void = { _ in }
    public var sendReview: () -> Void = {}
    public var hunk: (FileKey, Int, HunkAction) -> Void = { _, _, _ in }
    public var openInNvim: () -> Void = {}
    public var dismissError: () -> Void = {}

    public init() {}
}

public struct MainWindow: View {
    let scene: WindowScene
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme

    public init(scene: WindowScene, actions: WindowActions = WindowActions()) {
        self.scene = scene
        self.actions = actions
    }

    public var body: some View {
        let theme = Theme(scheme)
        VStack(spacing: 0) {
            ToolbarStrip(scene: scene, actions: actions)
            Rectangle().fill(theme(.surface)).frame(height: 1)
            if let banner = ConnectionBanner(scene.connection) {
                BannerView(banner: banner, reconnect: actions.reconnect)
            }
            HStack(spacing: 0) {
                if let review = scene.review {
                    if review.sidebarShown {
                        SidebarView(scene: scene, actions: actions)
                            .frame(width: Metrics.sidebarWidth)
                            .background(theme(.mantle))
                    } else {
                        RailView(scene: scene, actions: actions)
                            .frame(width: Metrics.railWidth)
                            .background(theme(.mantle))
                    }
                    Rectangle().fill(theme(.surface)).frame(width: 1)
                    ReviewView(review: review, actions: actions)
                    Rectangle().fill(theme(.surface)).frame(width: 1)
                    DraftPanel(review: review, actions: actions)
                        .frame(width: Metrics.inspectorWidth)
                        .background(theme(.mantle))
                } else {
                    SidebarView(scene: scene, actions: actions)
                        .frame(width: Metrics.sidebarWidth)
                        .background(theme(.mantle))
                    Rectangle().fill(theme(.surface)).frame(width: 1)
                    MainColumn(scene: scene)
                }
                if scene.review == nil, scene.inspector {
                    Rectangle().fill(theme(.surface)).frame(width: 1)
                    InspectorView(session: scene.session)
                        .frame(width: Metrics.inspectorWidth)
                        .background(theme(.mantle))
                }
            }
        }
        .foregroundStyle(theme(.text))
        .background(theme(.base))
    }
}

struct ToolbarStrip: View {
    let scene: WindowScene
    let actions: WindowActions
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        HStack(spacing: Metrics.gutter) {
            Image(systemName: "sidebar.left").foregroundStyle(theme(.subtext))
            VStack(alignment: .leading, spacing: 1) {
                Text(scene.session?.name ?? "agentws").font(.system(size: 13, weight: .semibold)).lineLimit(1)
                Text(scene.session?.where ?? "").font(Metrics.mono).foregroundStyle(theme(.subtext)).lineLimit(1)
            }
            .frame(minWidth: 160, alignment: .leading)
            HStack(spacing: 2) {
                ForEach(Toolbar.views) { tab in
                    let active = tab.title == (scene.review == nil ? "Terminal" : "Review")
                    Button {
                        if tab.enabled, !active { actions.toggleReview() }
                    } label: {
                        Text(tab.title)
                            .font(.system(size: 12, weight: active ? .semibold : .regular))
                            .padding(.horizontal, 10)
                            .padding(.vertical, 4)
                            .background(RoundedRectangle(cornerRadius: 5).fill(active ? theme(.base) : .clear))
                            .foregroundStyle(tab.enabled ? theme(.text) : theme(.grey))
                    }
                    .buttonStyle(.plain)
                    .disabled(!tab.enabled)
                }
            }
            .padding(2)
            .background(RoundedRectangle(cornerRadius: 7).fill(theme(.crust)))
            Spacer(minLength: Metrics.gutter)
            ForEach(QuotaMeter.all(scene.state?.limits ?? [], now: scene.now)) { meter in
                QuotaMeterView(meter: meter)
            }
            ServerChip(server: scene.server, connection: scene.connection)
            Label("New", systemImage: "plus")
                .font(.system(size: 12, weight: .medium))
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
                .background(RoundedRectangle(cornerRadius: 5).stroke(theme(.surface)))
                .foregroundStyle(Toolbar.newEnabled ? theme(.text) : theme(.grey))
            Button(action: actions.toggleInspector) {
                Image(systemName: "sidebar.right").foregroundStyle(scene.inspector ? theme(.blue) : theme(.subtext))
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, Metrics.gutter)
        .frame(height: Metrics.toolbarHeight)
        .background(theme(.mantle))
    }
}

struct QuotaMeterView: View {
    let meter: QuotaMeter
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        VStack(alignment: .leading, spacing: 2) {
            Text(meter.title).font(.system(size: 10, weight: .medium)).foregroundStyle(theme(.subtext))
            ZStack(alignment: .leading) {
                Capsule().fill(theme(.surface)).frame(width: 64, height: 5)
                Capsule().fill(theme(meter.tone)).frame(width: 64 * CGFloat(meter.used) / 100, height: 5)
            }
            Text(meter.caption).font(.system(size: 9)).foregroundStyle(theme(.subtext))
        }
        .opacity(meter.stale ? 0.45 : 1)
        .accessibilityElement(children: .combine)
    }
}

struct ServerChip: View {
    let server: String
    let connection: ConnectionStatus
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        let tone: Tone = switch connection {
        case .live: .green
        case .versionMismatch: .red
        case .idle: .grey
        default: .peach
        }
        HStack(spacing: 5) {
            Circle().fill(theme(tone)).frame(width: 7, height: 7)
            Text(server).font(.system(size: 12))
        }
        .padding(.horizontal, 9)
        .padding(.vertical, 4)
        .background(Capsule().fill(theme(.crust)))
    }
}

struct BannerView: View {
    let banner: ConnectionBanner
    let reconnect: () -> Void
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        HStack(spacing: 10) {
            Rectangle().fill(theme(banner.tone)).frame(width: 4)
            Image(systemName: banner.retrying ? "arrow.clockwise" : "exclamationmark.octagon.fill")
                .foregroundStyle(theme(banner.tone))
            Text(banner.title).font(.system(size: 12, weight: .semibold))
            Text(banner.detail).font(.system(size: 12)).foregroundStyle(theme(.subtext)).lineLimit(2)
            Spacer()
            if let action = banner.action {
                Button(action, action: reconnect)
                    .buttonStyle(.plain)
                    .font(.system(size: 12, weight: .semibold))
                    .padding(.horizontal, 10)
                    .padding(.vertical, 4)
                    .background(RoundedRectangle(cornerRadius: 5).fill(theme(banner.tone)))
                    .foregroundStyle(theme(.base))
            }
        }
        .padding(.trailing, Metrics.gutter)
        .frame(height: 36)
        .background(theme(banner.tone).opacity(0.14))
    }
}
#endif
