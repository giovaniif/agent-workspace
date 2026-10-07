#if canImport(SwiftUI)
import AgentwsKit
import Observation
import SwiftUI

@MainActor
@Observable
public final class WindowRouter {
    public var focus: String?
    public var focusSeq = 0
    public var shell: (session: String, worktree: String)?
    public var shellSeq = 0
    public var newSessionRequested = false

    public init() {}

    public func goTo(session: String) {
        focus = session
        focusSeq += 1
    }

    public func newSession() {
        newSessionRequested = true
    }

    public func openShell(session: String, worktree: String) {
        shell = (session, worktree)
        shellSeq += 1
    }
}

public struct LiveDiskWindow: View {
    let store: ViewStore
    let router: WindowRouter
    @State private var disk: DiskView?
    @State private var tab: DiskTab = .worktrees
    @State private var selected: String?
    @State private var message: String?
    @State private var pending: DiskAction?
    @Environment(\.openWindow) private var openWindow

    public init(store: ViewStore, router: WindowRouter) {
        self.store = store
        self.router = router
    }

    public var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { context in
            DiskWindow(
                scene: DiskScene(disk: disk, state: store.state, tab: tab, selected: selected, now: context.date, message: message),
                actions: actions
            )
        }
        .frame(minWidth: 820, minHeight: 480)
        .task { await poll() }
        .confirmationDialog(
            pending?.confirm ?? "",
            isPresented: Binding(get: { pending != nil }, set: { if !$0 { pending = nil } }),
            titleVisibility: .visible,
            presenting: pending
        ) { action in
            Button(action.title.replacingOccurrences(of: "…", with: ""), role: .destructive) { Task { await run(action) } }
            Button("Cancel", role: .cancel) {}
        } message: { action in
            Text(action.outcome)
        }
    }

    private var actions: DiskWindowActions {
        var a = DiskWindowActions()
        a.tab = { tab = $0 }
        a.select = { selected = $0 }
        a.perform = { action in
            if action.confirm != nil {
                pending = action
            } else {
                Task { await run(action) }
            }
        }
        return a
    }

    private func poll() async {
        store.start()
        while !Task.isCancelled {
            await load()
            let measuring = disk.map { $0.worktreesPending > 0 } ?? true
            try? await Task.sleep(for: .seconds(measuring ? 2 : 10))
        }
    }

    private func load() async {
        do {
            let view: DiskView = try await store.call("disk.view", params: [String: String]())
            disk = view
            if message?.hasPrefix("disk.view") == true { message = nil }
        } catch {
            message = "disk.view: \(error)"
        }
    }

    private func run(_ action: DiskAction) async {
        if action.kind == .killDevServers {
            await kill(action.pgids)
            return
        }
        let rows = disk.map { DiskTable.rows($0, state: store.state) } ?? []
        guard let row = rows.first(where: { $0.id == selected }) else { return }
        switch action.kind {
        case .goToSession:
            router.goTo(session: row.sessionID)
            openWindow(id: "main")
        case .openShell where !row.sessionID.isEmpty:
            router.goTo(session: row.sessionID)
            router.openShell(session: row.sessionID, worktree: row.id)
            openWindow(id: "main")
        case .openShell:
            do {
                let _: ShellResult = try await store.call("shell.toggle", params: ShellParams(session: row.sessionID, worktree: row.id))
                message = "Opened a shell in \(row.worktree)."
            } catch {
                message = "Open shell: \(error)"
            }
        case .killDevServers:
            await kill(action.pgids)
        case .remove, .backUpAndRemove:
            guard let params = action.params else { return }
            do {
                let item: CleanupItem = try await store.call("cleanup.worktree", params: params)
                message = "\(row.worktree): \(item.outcome ?? item.reason)"
            } catch {
                message = "Remove \(row.worktree): \(error)"
            }
            await load()
        }
    }

    private func kill(_ pgids: [Int]) async {
        do {
            let killed: PortsKilled = try await store.call("ports.kill", params: PortsKillParams(pgids: pgids))
            message = killed.killed.isEmpty ? "No dev server was stopped." : "Stopped \(killed.killed.count) dev server group(s)."
        } catch {
            message = "Kill dev servers: \(error)"
        }
    }
}
#endif
