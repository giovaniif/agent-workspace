#if canImport(SwiftUI)
import CrewKit
import SwiftUI

struct MateEditing: Identifiable {
    let id = UUID()
    let draft: MateDraft
    let isNew: Bool
}

struct MateConnecting: Identifiable {
    let id = UUID()
    let name: String
}

public struct LiveMatesWindow: View {
    let store: ViewStore
    let router: WindowRouter
    @State private var model: MatesModel?
    @State private var editing: MateEditing?
    @State private var connecting: MateConnecting?
    @Environment(\.openWindow) private var openWindow

    public init(store: ViewStore, router: WindowRouter) {
        self.store = store
        self.router = router
    }

    public var body: some View {
        MatesWindow(
            scene: MatesScene(
                board: store.state?.mates ?? MateBoard(), definitions: model?.definitions ?? [], problems: model?.problems ?? [],
                approvals: model?.approvals ?? [], message: model?.message
            ),
            actions: actions
        )
        .frame(minWidth: 720, minHeight: 480)
        .task {
            store.start()
            let m = MatesModel(caller: store)
            model = m
            await m.load()
        }
        .sheet(item: $editing) { edit in
            MateEditSheet(draft: edit.draft, isNew: edit.isNew, save: { draft in
                Task {
                    guard await model?.save(draft) == true else { return }
                    editing = nil
                    if edit.isNew {
                        await begin(draft.mate.name)
                    }
                }
            }, cancel: { editing = nil })
        }
        .sheet(item: $connecting) { c in
            MateConnectSheet(name: c.name, setup: model?.setup, claim: model?.claim, message: model?.message, connect: { bot, app in
                Task { await model?.connect(c.name, bot: bot, app: app) }
            }, done: { connecting = nil })
        }
    }

    private var actions: MatesActions {
        var a = MatesActions()
        a.pause = { row in Task { await model?.setPaused(row.name, paused: !row.paused) } }
        a.stopAll = { stop in Task { await model?.stopAll(stop) } }
        a.edit = { def in
            editing = MateEditing(draft: def.map { MateDraft($0) } ?? MateDraft(), isNew: def == nil)
        }
        a.connect = { name in Task { await begin(name) } }
        a.openSession = { session in
            router.goTo(session: session)
            openWindow(id: "main")
        }
        a.decide = { approval, approve in Task { await model?.decide(approval.id, approve: approve) } }
        return a
    }

    private func begin(_ name: String) async {
        await model?.startSetup(name)
        connecting = MateConnecting(name: name)
    }
}
#endif
