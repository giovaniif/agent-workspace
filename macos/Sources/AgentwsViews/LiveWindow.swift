#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

public struct LiveWindow: View {
    @State private var store: ViewStore
    @State private var nav = Navigator()
    @State private var filter = ""
    @State private var inspector = true
    @State private var endedExpanded = false
    @State private var focusFilter = 0
    @State private var review: ReviewController?
    private let server: String
    private let router: WindowRouter
#if canImport(SwiftTerm)
    @State private var terminals: (hub: TerminalHub, views: TerminalViews)
#endif
    private let keys: Shortcuts

    public init(store: ViewStore, server: String, router: WindowRouter = WindowRouter(), shortcuts: Shortcuts = .defaults) {
        _store = State(initialValue: store)
        self.server = server
        self.router = router
#if canImport(SwiftTerm)
        let hub = TerminalHub(endpoint: store.endpoint) { [store] cols, rows in
            try await store.call("client.native", params: NativeClientParams(cols: cols, rows: rows))
        }
        _terminals = State(initialValue: (hub, TerminalViews()))
#endif
        keys = shortcuts
    }

    public var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { context in
            let scene = WindowScene(
                state: store.state, connection: store.connection, selected: nav.selected, filter: filter,
                inspector: inspector, endedExpanded: endedExpanded, server: server, now: context.date, focusFilter: focusFilter,
                review: reviewScreen
            )
            MainWindow(scene: scene, actions: actions)
                .background { shortcuts(scene) }
        }
        .frame(minWidth: 900, minHeight: 560)
#if canImport(SwiftTerm)
        .environment(\.agentwsTerminals, terminals)
        .onAppear { terminals.hub.start() }
#endif
        .onAppear { store.start() }
        .onChange(of: store.state?.seq) { reconcile(filter: filter) }
        .onChange(of: router.focusSeq, initial: true) { if let id = router.focus { nav.select(id) } }
        .onChange(of: nav.selected) {
            if review != nil { openReview() }
        }
    }

    private var reviewScreen: ReviewScreen? {
        guard var screen = review?.screen else { return nil }
        if let state = store.state?.sessions.first(where: { $0.id == screen.session })?.state {
            screen.sessionState = state
        }
        return screen
    }

    private func openReview() {
        guard let id = nav.selected else {
            review = nil
            return
        }
        let previous = review?.screen
        let controller = ReviewController(session: id, caller: store)
        if let previous {
            controller.screen.scope = previous.scope
            controller.screen.layout = previous.layout
            controller.screen.sidebarShown = previous.sidebarShown
        }
        review = controller
        Task { await controller.open() }
    }

    private func toggleReview() {
        if review == nil { openReview() } else { review = nil }
    }

    private func reviewTask(_ work: @escaping @MainActor (ReviewController) async -> Void) {
        guard let review else { return }
        Task { await work(review) }
    }

    private func openInNvim() {
        guard let review, let file = review.screen.selectedFile else { return }
        let line = review.screen.composer.flatMap { CommentAnchor(lines: $0.lines)?.start }
            ?? file.file.hunks.first?.lines.first { $0.new > 0 }?.new ?? 1
        Task {
            if await review.openInNvim(file.key, line: line), self.review === review { self.review = nil }
        }
    }

    private var actions: WindowActions {
        var a = WindowActions()
        a.select = { nav.select($0) }
        a.filter = { value in
            filter = value
            reconcile(filter: value)
        }
        a.toggleInspector = { inspector.toggle() }
        a.toggleEnded = { endedExpanded.toggle() }
        a.reconnect = {
            store.stop()
            store.start()
#if canImport(SwiftTerm)
            terminals.hub.stop()
            terminals.hub.start()
#endif
        }
        a.toggleReview = { toggleReview() }
        a.toggleSidebar = { review?.screen.sidebarShown.toggle() }
        a.setScope = { scope in
            reviewTask { r in
                r.screen.scope = scope
                await r.open()
            }
        }
        a.stepScope = { by in reviewTask { await $0.stepScope(by) } }
        a.setWorktree = { review?.screen.worktree = $0 }
        a.setLayout = { review?.screen.layout = $0 }
        a.selectFile = { review?.select($0) }
        a.toggleViewed = { key in reviewTask { await $0.toggleViewed(key) } }
        a.startComment = { key, line in review?.screen.startComment(key, line) }
        a.extendComment = { review?.screen.extendComment(to: $0) }
        a.editComment = { review?.screen.composer?.text = $0 }
        a.cancelComment = { review?.screen.composer = nil }
        a.submitComment = {
            guard let composer = review?.screen.composer else { return }
            reviewTask { await $0.comment(on: composer.key, lines: composer.lines, body: composer.text) }
        }
        a.editNote = { review?.screen.note = $0 }
        a.sendReview = { reviewTask { await $0.send() } }
        a.hunk = { key, index, action in reviewTask { await $0.hunk(key, index: index, action: action) } }
        a.openInNvim = { openInNvim() }
        a.dismissError = { review?.screen.error = nil }
        return a
    }

    private func reconcile(filter: String) {
        if let state = store.state { nav.reconcile(with: Sidebar(state: state, filter: filter)) }
    }

    @ViewBuilder
    private func shortcuts(_ scene: WindowScene) -> some View {
        let sidebar = scene.sidebar
        ZStack {
            ForEach(1...9, id: \.self) { n in
                Button("") { if let sidebar { nav.jump(to: n, in: sidebar) } }
                    .keyboardShortcut(KeyEquivalent(Character("\(n)")), modifiers: .command)
            }
            bound(.nextWaiting) { if let sidebar { nav.nextWaiting(in: sidebar) } }
            bound(.lastSession) { nav.last() }
            bound(.filter) { focusFilter += 1 }
            bound(.inspector) { inspector.toggle() }
            bound(.review) { toggleReview() }
            Button("") { review?.screen.sidebarShown.toggle() }
                .keyboardShortcut("s", modifiers: [.command, .control])
            Button("") { reviewTask { await $0.send() } }
                .keyboardShortcut(.return, modifiers: [.command, .shift])
            bound(.nvimAtFile) { openInNvim() }
        }
        .opacity(0)
        .accessibilityHidden(true)
    }

    @ViewBuilder
    private func bound(_ action: ShortcutAction, _ perform: @escaping () -> Void) -> some View {
        if let combo = keys.combo(for: action) {
            Button("", action: perform).keyboardShortcut(combo.equivalent, modifiers: combo.eventModifiers)
        }
    }
}
#endif
