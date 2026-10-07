import Foundation
import Observation

public enum MainView: String, Sendable, Equatable, CaseIterable {
    case terminal = "Terminal"
    case review = "Review"
    case shell = "Shell"
    case nvim = "nvim"
}

struct ShellFocusParams: Encodable, Sendable {
    var session: String
    var worktree: String
}

struct NvimToggleParams: Encodable, Sendable {
    var session: String
}

@MainActor
@Observable
public final class ShellNvim {
    public private(set) var view: MainView = .terminal
    public private(set) var popup: String?
    public private(set) var shells: [String: String] = [:]
    public private(set) var nvims: [String: String] = [:]
    public var error: String?
    private let caller: Caller
    private var pending: MainView?
    private var generation = 0

    public init(caller: Caller) {
        self.caller = caller
    }

    public func pane(session: String, agent: String) -> String {
        switch view {
        case .shell: shells[session] ?? agent
        case .nvim: nvims[session] ?? agent
        case .terminal, .review: agent
        }
    }

    public func toggleShell(session: String, worktree: String = "", popup: Bool = false) async {
        if popup, self.popup != nil {
            self.popup = nil
            return
        }
        if !popup, view == .shell || pending == .shell {
            close()
            return
        }
        await openShell(session: session, worktree: worktree, popup: popup)
    }

    public func openShell(session: String, worktree: String = "", popup: Bool = false) async {
        let ticket = begin(popup ? nil : .shell)
        do {
            let opened: ShellResult = try await caller.call("shell.focus", params: ShellFocusParams(session: session, worktree: worktree))
            guard let pane = opened.pane, !pane.isEmpty else { return }
            shells[session] = pane
            guard current(ticket) else { return }
            error = nil
            if popup {
                self.popup = pane
            } else {
                view = .shell
            }
        } catch {
            if current(ticket) { self.error = ReviewController.message(error) }
        }
    }

    public func toggleNvim(session: String) async {
        if view == .nvim || pending == .nvim {
            close()
            return
        }
        let ticket = begin(.nvim)
        do {
            let opened: NvimOpened = try await caller.call("nvim.toggle", params: NvimToggleParams(session: session))
            nvims[session] = opened.pane
            if current(ticket) { showNvim(session: session, pane: opened.pane) }
        } catch {
            if current(ticket) { self.error = ReviewController.message(error) }
        }
    }

    private func begin(_ view: MainView?) -> Int {
        generation += 1
        if let view { pending = view }
        return generation
    }

    private func current(_ ticket: Int) -> Bool {
        guard ticket == generation else { return false }
        pending = nil
        return true
    }

    public func showNvim(session: String, pane: String) {
        generation += 1
        pending = nil
        nvims[session] = pane
        error = nil
        view = .nvim
    }

    public func closePopup() {
        popup = nil
    }

    public func close() {
        generation += 1
        pending = nil
        view = .terminal
        popup = nil
    }
}
