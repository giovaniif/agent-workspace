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
    @State private var newSession: NewSession?
    @State private var newSessionTab = NewSessionTab.session
    @State private var panes: ShellNvim
    @State private var windowID = UUID().uuidString
    @Environment(\.controlActiveState) private var activeState
    private let server: String
    private let router: WindowRouter
    private let attention: Attention?
#if canImport(SwiftTerm)
    @State private var terminals: (hub: TerminalHub, views: TerminalViews)
#endif
    private let keys: Shortcuts

    public init(
        store: ViewStore, server: String, router: WindowRouter = WindowRouter(), shortcuts: Shortcuts = .defaults,
        attention: Attention? = nil
    ) {
        _store = State(initialValue: store)
        self.server = server
        self.router = router
        _panes = State(initialValue: ShellNvim(caller: store))
        self.attention = attention
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
                review: reviewScreen, view: panes.view,
                pane: nav.selected.map { panes.pane(session: $0, agent: "") }.flatMap { $0.isEmpty ? nil : $0 },
                popup: panes.popup, paneError: panes.error, card: attention?.card, message: attention?.message
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
        .onChange(of: store.state?.seq) {
            reconcile(filter: filter)
            newSession?.observe(store.state)
        }
        .onChange(of: router.focusSeq, initial: true) { if let id = router.focus { nav.select(id) } }
        .onChange(of: router.newSessionRequested, initial: true) {
            guard router.newSessionRequested else { return }
            router.newSessionRequested = false
            openNewSession(.session)
        }
        .onChange(of: router.shellSeq, initial: true) {
            guard let shell = router.shell else { return }
            router.shell = nil
            review = nil
            Task { await panes.openShell(session: shell.session, worktree: shell.worktree) }
        }
        .onChange(of: nav.selected) {
            panes.close()
            if review != nil { openReview() }
        }
        .onChange(of: viewingKey, initial: true) { reportViewing() }
        .onChange(of: selectedState, initial: true) { refreshCard() }
        .onChange(of: store.connection) {
            if store.connection == .live, let attention { Task { await attention.reconnected() } }
        }
        .onDisappear {
            let window = windowID
            if let attention { Task { await attention.view(session: nil, front: false, window: window) } }
        }
        .sheet(item: $newSession) { model in
            NewSessionSheet(
                model: model, tab: $newSessionTab, server: server, state: store.state,
                started: { id in
                    newSession = nil
                    nav.select(id)
                },
                cancel: { newSession = nil }
            )
        }
    }

    private func openNewSession(_ tab: NewSessionTab) {
        newSessionTab = tab
        guard newSession == nil else { return }
        let model = NewSession(caller: store)
        newSession = model
        Task { await model.load(state: store.state) }
    }

    private var front: Bool { activeState == .key }

    private var viewingKey: String { (nav.selected ?? "") + (front ? "|front" : "|back") }

    private var selectedState: String {
        guard let id = nav.selected, let session = store.state?.sessions.first(where: { $0.id == id }) else { return "" }
        return id + "|" + session.state
    }

    private func reportViewing() {
        guard let attention else { return }
        let session = nav.selected
        let front = front
        let window = windowID
        Task { await attention.view(session: session, front: front, window: window) }
    }

    private func refreshCard() {
        guard let attention else { return }
        let id = nav.selected
        let state = id.flatMap { id in store.state?.sessions.first { $0.id == id }?.state }
        Task { await attention.refreshCard(session: id, state: state) }
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
            guard let pane = await review.openInNvim(file.key, line: line), self.review === review else { return }
            self.review = nil
            panes.showNvim(session: review.screen.session, pane: pane)
        }
    }

    private func toggleShell(popup: Bool) {
        guard let id = nav.selected else { return }
        if !popup { review = nil }
        Task { await panes.toggleShell(session: id, popup: popup) }
    }

    private func editor() {
        if review != nil {
            openInNvim()
            return
        }
        guard let id = nav.selected else { return }
        Task { await panes.toggleNvim(session: id) }
    }

    private func show(_ view: MainView) {
        switch view {
        case .terminal:
            review = nil
            panes.close()
        case .review:
            panes.close()
            if review == nil { openReview() }
        case .shell:
            toggleShellView()
        case .nvim:
            review = nil
            guard let id = nav.selected, panes.view != .nvim else { return }
            Task { await panes.toggleNvim(session: id) }
        }
    }

    private func toggleShellView() {
        review = nil
        guard let id = nav.selected, panes.view != .shell else { return }
        Task { await panes.openShell(session: id) }
    }

    private var actions: WindowActions {
        var a = WindowActions()
        a.select = { nav.select($0) }
        a.filter = { value in
            filter = value
            reconcile(filter: value)
        }
        a.toggleInspector = { inspector.toggle() }
        a.newSession = { openNewSession(.session) }
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
        a.showView = { show($0) }
        a.closePopup = { panes.closePopup() }
        a.dismissError = { review?.screen.error = nil }
        a.answer = { choice in
            if let attention { Task { await attention.answer(choice: choice) } }
        }
        a.hideCard = { attention?.hideCard() }
        a.dismissMessage = { attention?.message = nil }
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
            bound(.nvimAtFile) { editor() }
            bound(.newSession) { openNewSession(.session) }
            bound(.linearLauncher) { openNewSession(.launcher) }
            bound(.shellSplit) { toggleShell(popup: false) }
            bound(.shellPopup) { toggleShell(popup: true) }
        }
        .opacity(0)
        .accessibilityHidden(true)
    }

    @ViewBuilder
    private func bound(_ action: ShortcutAction, _ perform: @escaping () -> Void) -> some View {
        if let combo = keys.combo(for: action), combo.isBindable {
            Button("", action: perform).keyboardShortcut(combo.equivalent, modifiers: combo.eventModifiers)
        }
    }
}
#endif
