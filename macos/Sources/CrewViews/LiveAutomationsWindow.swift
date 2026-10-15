#if canImport(SwiftUI)
import CrewKit
import SwiftUI

struct AutomationEditing: Identifiable {
    let id = UUID()
    let draft: AutomationDraft
}

public struct LiveAutomationsWindow: View {
    let store: ViewStore
    let router: WindowRouter
    @State private var model: AutomationsModel?
    @State private var query = AutomationQuery()
    @State private var selected: String?
    @State private var tab: AutomationTab = .overview
    @State private var running: Automation?
    @State private var editing: AutomationEditing?
    @Environment(\.openWindow) private var openWindow

    public init(store: ViewStore, router: WindowRouter) {
        self.store = store
        self.router = router
    }

    public var body: some View {
        AutomationsWindow(
            scene: AutomationsScene(
                automations: model?.list ?? [], runs: model?.runs ?? [], problems: model?.problems ?? [],
                query: query, selected: selected, tab: tab, message: model?.message
            ),
            actions: actions
        )
        .frame(minWidth: 900, minHeight: 520)
        .task {
            store.start()
            let m = AutomationsModel(caller: store)
            model = m
            await m.load()
            if selected == nil { selected = AutomationTable.rows(m.list, runs: m.runs, query: query).first?.id }
        }
        .sheet(item: $running) { automation in
            AutomationRunSheet(automation: automation, run: { context, workItem in
                running = nil
                Task { await start(automation, context: context, workItem: workItem) }
            }, cancel: { running = nil })
        }
        .sheet(item: $editing) { edit in
            AutomationEditSheet(draft: edit.draft, configPath: "~/.crew/config.toml", save: { draft in
                editing = nil
                Task {
                    if await model?.save(draft) == true {
                        selected = draft.automation.id
                    }
                }
            }, cancel: { editing = nil })
        }
    }

    private var actions: AutomationsActions {
        var a = AutomationsActions()
        a.query = { query = $0 }
        a.select = { selected = $0 }
        a.tab = { tab = $0 }
        a.run = { running = $0 }
        a.rerun = { run in
            Task {
                if let out = await model?.rerun(run.id), let session = out.sessionID {
                    open(session)
                }
            }
        }
        a.openSession = { open($0) }
        a.edit = { automation in
            editing = AutomationEditing(draft: automation.map { AutomationDraft($0) } ?? AutomationDraft())
        }
        return a
    }

    private func start(_ automation: Automation, context: String, workItem: String) async {
        if let out = await model?.run(automation.name, context: context, workItem: workItem), let session = out.sessionID {
            open(session)
        }
    }

    private func open(_ session: String) {
        router.goTo(session: session)
        openWindow(id: "main")
    }
}
#endif
