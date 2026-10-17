#if canImport(SwiftUI)
import CrewKit
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

enum ProfilePrompt {
    case new, clone
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
    @State private var panes: ShellNvim
    @State private var windowID = UUID().uuidString
    @State private var commands: SessionCommands
    @State private var explanation: String?
    @State private var prompt: SessionPrompt?
    @State private var renameText = ""
    @State private var choiceRequest = 0
    @State private var browser = Browser()
    @State private var profiles = BrowserProfiles()
    @State private var profilePrompt: ProfilePrompt?
    @State private var profileName = ""
    @State private var headless: HeadlessView
    @State private var serverSend: ServerProfileSend
#if canImport(WebKit)
    @State private var webViews = BrowserWebViews()
    @State private var cookieImport: BrowserProfile?
    @State private var serverCookies: BrowserProfile?
    @State private var agentLink: Task<Void, Never>?
#endif
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

    private let servers: [ServerKind]
    private let switchServer: (ServerKind) -> Void
    @Environment(\.openWindow) private var openWindow

    public init(
        store: ViewStore, server: String, router: WindowRouter = WindowRouter(), shortcuts: Shortcuts = .defaults,
        attention: Attention? = nil, diffLayout: DiffLayout = .unified, confirmEnd: Bool = true,
        servers: [ServerKind] = [], switchServer: @escaping (ServerKind) -> Void = { _ in }
    ) {
        self.servers = servers
        self.switchServer = switchServer
        _store = State(initialValue: store)
        self.server = server
        self.router = router
        _panes = State(initialValue: ShellNvim(caller: store))
        _commands = State(initialValue: SessionCommands(caller: store))
        _headless = State(initialValue: HeadlessView(caller: store))
        _serverSend = State(initialValue: ServerProfileSend(caller: store))
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

    private var windowMessage: String? {
        explanation ?? attention?.message ?? commands.message
    }

    private func makeScene(at now: Date) -> WindowScene {
        let pane = nav.selected.map { panes.pane(session: $0, agent: "") }.flatMap { $0.isEmpty ? nil : $0 }
        var scene = WindowScene(
            state: store.state, connection: store.connection, selected: nav.selected, filter: filter,
            inspector: inspector, endedExpanded: endedExpanded, server: server, now: now, focusFilter: focusFilter,
            review: reviewScreen, view: panes.view, pane: pane,
            popup: panes.popup, paneError: panes.error, card: attention?.card, message: windowMessage, servers: servers
        )
        scene.browser = browserChrome()
        return scene
    }

    private var profileAlertTitle: String {
        profilePrompt == .clone ? "Clone profile" : "New browser profile"
    }

    private var profileAlertShown: Binding<Bool> {
        Binding(get: { profilePrompt != nil }, set: { if !$0 { profilePrompt = nil } })
    }

    private func browserLayer(_ content: some View) -> some View {
#if canImport(WebKit)
        let layered = content
            .environment(\.crewBrowser, webViews)
            .onAppear {
                bindWebViews()
                startBrowserLink()
            }
            .onDisappear {
                agentLink?.cancel()
                agentLink = nil
            }
            .sheet(item: $cookieImport) { profile in
                CookieImportSheet(profile: profile, store: webViews.store(for: profile.id).httpCookieStore) { cookieImport = nil }
            }
            .sheet(item: $serverCookies) { profile in
                CookieImportSheet(profile: profile, store: webViews.store(for: profile.id).httpCookieStore, target: .server(serverSend)) { serverCookies = nil }
            }
#else
        let layered = content
#endif
        return layered.alert(profileAlertTitle, isPresented: profileAlertShown) {
            TextField("Name", text: $profileName)
            Button("Create") { createProfile() }
                .keyboardShortcut(.defaultAction)
            Button("Cancel", role: .cancel) { profilePrompt = nil }
        }
    }

    public var body: some View {
        browserLayer(window)
    }

    private var window: some View {
        TimelineView(.periodic(from: .now, by: 1)) { context in
            let scene = makeScene(at: context.date)
            MainWindow(scene: scene, actions: actions)
                .background { shortcuts(scene) }
        }
        .frame(minWidth: 900, minHeight: 560)
#if canImport(SwiftTerm)
        .environment(\.crewTerminals, terminals)
        .onAppear { terminals.hub.start() }
        .onChange(of: keys.bound, initial: true) { terminals.views.bound = keys.bound }
#endif
        .onAppear { store.start() }
        .task(id: headlessKey) { await watchHeadless(headlessKey) }
        .onChange(of: store.state?.seq) {
            reconcile(filter: filter)
            newSession?.observe(store.state)
        }
        .onChange(of: router.focusSeq, initial: true) { if let id = router.focus { nav.select(id) } }
        .onChange(of: router.newSessionRequested, initial: true) {
            guard router.newSessionRequested else { return }
            router.newSessionRequested = false
            openNewSession()
        }
        .onChange(of: router.shellSeq, initial: true) {
            guard let shell = router.shell else { return }
            router.shell = nil
            review = nil
            Task { await panes.openShell(session: shell.session, worktree: shell.worktree) }
        }
        .onChange(of: nav.selected) {
            explanation = nil
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
                model: model, server: server, state: store.state,
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

    private func openNewSession() {
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
        guard nav.selected != nil || review != nil else { return explain(.review) }
        if review == nil { openReview() } else { review = nil }
    }

    private func explain(_ view: MainView) {
        explanation = Toolbar.needsSession(view)
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
        guard let id = nav.selected else { return explain(.shell) }
        if !popup { review = nil }
        Task { await panes.toggleShell(session: id, popup: popup) }
    }

    private func editor() {
        if review != nil {
            openInNvim()
            return
        }
        guard let id = nav.selected else { return explain(.nvim) }
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
        case .browser:
            review = nil
            panes.showBrowser()
        }
    }

    private var browserHost: String {
        BrowserHost.of(servers.first { $0.name == server } ?? .thisMac)
    }

    private func browserChrome() -> BrowserChrome? {
        guard panes.view == .browser, let id = nav.selected else { return nil }
        let links = store.state.map { BrowserLinks.ports(session: id, state: $0, host: browserHost) } ?? []
        var chrome = BrowserChrome(page: browser.page(session: id), profile: profiles.profile(for: id), profiles: profiles.profiles, links: links)
        chrome.headless = headless.frame(for: id)
        return chrome
    }

    private var headlessKey: String? {
        guard panes.view == .browser, store.connection == .live else { return nil }
        return nav.selected
    }

    private func watchHeadless(_ session: String?) async {
        guard let session else {
            headless.clear()
            return
        }
        while !Task.isCancelled {
            await headless.refresh(session: session)
            try? await Task.sleep(for: HeadlessView.interval)
        }
    }

#if canImport(WebKit)
    private func startBrowserLink() {
        guard agentLink == nil else { return }
        let host = browserHost
        let driver = BrowserDriver(browser: browser, profiles: profiles, webViews: webViews) { host }
        let link = BrowserLink(endpoint: store.endpoint, build: store.build) { call in
            await driver.run(call)
        }
        agentLink = Task { await link.run() }
    }

    private func bindWebViews() {
        let browser = browser
        let profiles = profiles
        webViews.report = { tab, url, title in
            browser.pages.values.first { page in page.tabs.contains { $0.id == tab } }?.update(tab, url: url, title: title)
        }
        webViews.loading = { tab in
            browser.pages.values.first { page in page.tabs.contains { $0.id == tab } }?.loading(tab)
        }
        webViews.profile = { tab in
            let session = browser.pages.first { $0.value.tabs.contains { $0.id == tab } }?.key ?? ""
            return profiles.profile(for: session).id
        }
    }
#endif

    private func browse(_ action: BrowserAction) {
        guard let id = nav.selected else { return explain(.browser) }
        let page = browser.page(session: id)
        switch action {
        case let .navigate(input):
            if !page.navigate(input, host: browserHost) { explanation = "Not an address: " + input }
        case let .open(url):
            page.open(url)
        case let .select(tab):
            page.select(tab)
        case let .close(tab):
            page.close(tab)
#if canImport(WebKit)
            webViews.close(tab)
#endif
        case .back, .forward, .reload:
#if canImport(WebKit)
            let web = webViews.webView(for: page.selected)
            if action == .back { web?.goBack() } else if action == .forward { web?.goForward() } else { web?.reload() }
#endif
        case let .device(device):
            page.device = device
        case let .profile(profile):
            profiles.assign(session: id, profile: profile)
            reopenTabs(page)
        case .newProfile:
            profileName = ""
            profilePrompt = .new
        case .cloneProfile:
            profileName = profiles.profile(for: id).name + " copy"
            profilePrompt = .clone
        case .makeDefault:
            profiles.setDefault(profiles.profile(for: id).id)
        case .importCookies:
#if canImport(WebKit)
            cookieImport = profiles.profile(for: id)
#endif
        case .sendToServer:
#if canImport(WebKit)
            serverCookies = profiles.profile(for: id)
#endif
        case .takeOver:
            page.takeOver()
        }
    }

    private func reopenTabs(_ page: BrowserPage) {
#if canImport(WebKit)
        for tab in page.tabs { webViews.close(tab.id) }
#endif
    }

    private func createProfile() {
        guard let kind = profilePrompt, let id = nav.selected else { return }
        profilePrompt = nil
        let source = profiles.profile(for: id)
        do {
            let made = try (kind == .clone ? profiles.clone(source.id, name: profileName) : profiles.create(profileName))
            let page = browser.page(session: id)
#if canImport(WebKit)
            if kind == .clone {
                let webViews = webViews
                let profiles = profiles
                Task {
                    await webViews.clone(from: source.id, to: made.id)
                    profiles.assign(session: id, profile: made.id)
                    for tab in page.tabs { webViews.close(tab.id) }
                }
                return
            }
#endif
            profiles.assign(session: id, profile: made.id)
            reopenTabs(page)
        } catch ProfileRefusal.taken(let name) {
            explanation = "A profile named \(name) already exists"
        } catch {
            explanation = "A profile needs a name"
        }
    }

    private func toggleShellView() {
        review = nil
        guard let id = nav.selected, panes.view != .shell else { return }
        Task { await panes.openShell(session: id) }
    }

    private var actions: WindowActions {
        var a = WindowActions()
        a.switchServer = switchServer
        a.addServer = { openWindow(id: "setup") }
        a.select = { nav.select($0) }
        a.filter = { value in
            filter = value
            reconcile(filter: value)
        }
        a.toggleInspector = { inspector.toggle() }
        a.newSession = { openNewSession() }
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
        a.browser = { browse($0) }
        a.closePopup = { panes.closePopup() }
        a.dismissError = { review?.screen.error = nil }
        a.answer = { choice in
            if let attention { Task { await attention.answer(choice: choice) } }
        }
        a.hideCard = { attention?.hideCard() }
        a.explain = { explanation = $0 }
        a.dismissMessage = {
            if explanation != nil {
                explanation = nil
                return
            }
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
            bound(.newSession) { openNewSession() }
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
