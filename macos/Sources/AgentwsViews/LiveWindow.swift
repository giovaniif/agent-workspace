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
    private let server: String
    private let router: WindowRouter

    public init(store: ViewStore, server: String, router: WindowRouter = WindowRouter()) {
        _store = State(initialValue: store)
        self.server = server
        self.router = router
    }

    public var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { context in
            let scene = WindowScene(
                state: store.state, connection: store.connection, selected: nav.selected, filter: filter,
                inspector: inspector, endedExpanded: endedExpanded, server: server, now: context.date, focusFilter: focusFilter
            )
            MainWindow(scene: scene, actions: actions)
                .background { shortcuts(scene) }
        }
        .frame(minWidth: 900, minHeight: 560)
        .onAppear { store.start() }
        .onChange(of: store.state?.seq) { reconcile(filter: filter) }
        .onChange(of: router.focusSeq, initial: true) { if let id = router.focus { nav.select(id) } }
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
        }
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
            Button("") { if let sidebar { nav.nextWaiting(in: sidebar) } }
                .keyboardShortcut(.space, modifiers: .control)
            Button("") { nav.last() }
                .keyboardShortcut("[", modifiers: .command)
            Button("") { focusFilter += 1 }
                .keyboardShortcut("k", modifiers: .command)
            Button("") { inspector.toggle() }
                .keyboardShortcut("i", modifiers: [.command, .option])
        }
        .opacity(0)
        .accessibilityHidden(true)
    }
}
#endif
