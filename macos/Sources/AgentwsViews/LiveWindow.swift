#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

enum SessionPrompt {
    case rename(String)
    case choose(String, SwitchKind, [String])
    case end(String)

    enum Kind {
        case rename, choose, end
    }

    var kind: Kind {
        switch self {
        case .rename: .rename
        case .choose: .choose
        case .end: .end
        }
    }
}

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
    @State private var commands: SessionCommands
    @State private var prompt: SessionPrompt?
    @State private var renameText = ""
    @State private var choiceRequest = 0
    @Environment(\.controlActiveState) private var activeState
    private let server: String
    private let router: WindowRouter
    private let attention: Attention?
#if canImport(SwiftTerm)
    @State private var terminals: (hub: TerminalHub, views: TerminalViews)
#endif
    private let keys: Shortcuts
    private let diffLayout: DiffLayout
    private let confirmEnd: Bool

    public init(
        store: ViewStore, server: String, router: WindowRouter = WindowRouter(), shortcuts: Shortcuts = .defaults,
        attention: Attention? = nil, diffLayout: DiffLayout = .unified, confirmEnd: Bool = true
    ) {
        _store = State(initialValue: store)
        self.server = server
        self.router = router
        _panes = State(initialValue: ShellNvim(caller: store))
        _commands = State(initialValue: SessionCommands(caller: store))
        self.attention = attention
#if canImport(SwiftTerm)
        let hub = TerminalHub(endpoint: store.endpoint) { [store] cols, rows in
            try await store.call("client.native", params: NativeClientParams(cols: cols, rows: rows))
        }
        let views = TerminalViews()
        views.bound = shortcuts.bound
        _terminals = State(initialValue: (hub, views))
#endif
        keys = shortcuts
        self.diffLayout = diffLayout
        self.confirmEnd = confirmEnd
    }

    public var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { context in
            let scene = WindowScene(
                state: store.state, connection: store.connection, selected: nav.selected, filter: filter,
                inspector: inspector, endedExpanded: endedExpanded, server: server, now: context.date, focusFilter: focusFilter,
                review: reviewScreen, view: panes.view,
                pane: nav.selected.map { panes.pane(session: $0, agent: "") }.flatMap { $0.isEmpty ? nil : $0 },
                popup: panes.popup, paneError: panes.error, card: attention?.card, message: attention?.message ?? commands.message
            )
            MainWindow(scene: scene, actions: actions)
                .background { shortcuts(scene) }
        }
        .frame(minWidth: 900, minHeight: 560)
#if canImport(SwiftTerm)
        .environment(\.agentwsTerminals, terminals)
        .onAppear { terminals.hub.start() }
        .onChange(of: keys.bound, initial: true) { terminals.views.bound = keys.bound }
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
            choiceRequest += 1
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
                    review = nil
#if canImport(SwiftTerm)
                    terminals.views.focusNextShown()
#endif
                    nav.select(id)
                },
                cancel: { newSession = nil }
            )
        }
        .alert("Rename and pin", isPresented: presenting(.rename)) {
            TextField("Name", text: $renameText)
            Button("Rename") {
                if case let .rename(id)? = prompt { Task { await commands.rename(id, to: renameText) } }
                prompt = nil
            }
            .keyboardShortcut(.defaultAction)
            Button("Cancel", role: .cancel) { prompt = nil }
        }
        .confirmationDialog(choiceTitle, isPresented: presenting(.choose), titleVisibility: .visible) {
            if case let .choose(id, kind, values)? = prompt {
                ForEach(values, id: \.self) { value in
                    Button(value) { Task { await commands.switchTo(id, kind: kind, value: value) } }
                }
            }
            Button("Cancel", role: .cancel) { prompt = nil }
        }
        .confirmationDialog("End this session?", isPresented: presenting(.end), titleVisibility: .visible) {
            Button("End session", role: .destructive) {
                if case let .end(id)? = prompt { Task { await commands.end(id) } }
                prompt = nil
            }
            Button("Cancel", role: .cancel) { prompt = nil }
        } message: {
            Text("The agent stops; its worktrees stay. Resume it later from Ended.")
        }
    }

    private func presenting(_ kind: SessionPrompt.Kind) -> Binding<Bool> {
        Binding(get: { prompt?.kind == kind }, set: { if !$0 { prompt = nil } })
    }

    private var choiceTitle: String {
        guard case let .choose(_, kind, _)? = prompt else { return "" }
        return kind == .model ? "Switch model" : "Switch effort"
    }

    private func onSelected(_ item: SessionMenuItem) {
        guard let id = nav.selected else { return }
        perform(item, on: id)
    }

    private func perform(_ item: SessionMenuItem, on id: String) {
        guard let state = store.state, let session = state.sessions.first(where: { $0.id == id }) else { return }
        switch item {
        case .rename:
            renameText = session.name
            prompt = .rename(id)
        case .model, .effort:
            let kind: SwitchKind = item == .model ? .model : .effort
            choiceRequest += 1
            let request = choiceRequest
            Task {
                let values = await commands.choices(for: session, kind: kind)
                guard request == choiceRequest, prompt == nil, !values.isEmpty else { return }
                prompt = .choose(id, kind, values)
            }
        case .mute:
            Task { await commands.toggleMute(session) }
        case .end:
            guard !session.ended else { return }
            if confirmEnd {
                prompt = .end(id)
            } else {
                Task { await commands.end(id) }
            }
        case .resume:
            guard session.ended else { return }
            Task { await commands.resume(id) }
        case .killDevServers:
            Task { await commands.killDevServers(id, state: state) }
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
        let controller = ReviewController(session: id, caller: store, layout: diffLayout)
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
        a.dismissMessage = {
            attention?.message = nil
            commands.message = nil
        }
        a.sessionMenu = { item, id in perform(item, on: id) }
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
            bound(.rename) { onSelected(.rename) }
            bound(.model) { onSelected(.model) }
            bound(.effort) { onSelected(.effort) }
            bound(.mute) { onSelected(.mute) }
            bound(.endSession) { onSelected(.end) }
            bound(.resumeEnded) { onSelected(.resume) }
            bound(.killDevServers) { onSelected(.killDevServers) }
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
