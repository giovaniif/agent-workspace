import Foundation
import Observation

@MainActor
public protocol Caller: AnyObject {
    func call<Params: Encodable & Sendable, Result: Decodable & Sendable>(_ method: String, params: Params) async throws -> Result
}

extension ViewStore: Caller {}

@MainActor
@Observable
public final class ReviewController {
    public var screen: ReviewScreen
    private let caller: Caller

    public init(session: String, sessionState: String = "idle", caller: Caller) {
        screen = ReviewScreen(session: session, sessionState: sessionState)
        self.caller = caller
    }

    static func message(_ error: Error) -> String {
        switch error {
        case let AgentwsError.rpc(e): e.message
        case AgentwsError.disconnected: "Lost the connection to the daemon."
        case let AgentwsError.badReply(detail): detail
        default: String(describing: error)
        }
    }

    public func open() async {
        do {
            let result: ReviewResult = try await caller.call(
                "review.open", params: ReviewOpenParams(session: screen.session, scope: screen.scope, worktree: "")
            )
            screen.result = result
            screen.viewed = result.viewed
            if let draft = result.draft, !draft.id.isEmpty || !draft.comments.isEmpty { screen.draft = draft }
            screen.selected = screen.selectedFile?.key
            screen.error = nil
        } catch {
            screen.error = Self.message(error)
        }
    }

    public func stepScope(_ by: Int) async {
        screen.scope = screen.scope.step(by)
        await open()
    }

    public func select(_ key: FileKey) {
        screen.selected = key
        screen.composer = nil
    }

    public func toggleViewed(_ key: FileKey) async {
        guard let found = screen.file(key) else { return }
        let mark = ViewedMark(worktree: key.worktree, path: key.path, blob: found.file.blob)
        let viewed = !screen.isViewed(key, blob: found.file.blob)
        do {
            let _: Empty = try await caller.call("review.viewed", params: ReviewViewedParams(mark: mark, viewed: viewed))
            screen.viewed.removeAll { $0.worktree == key.worktree && $0.path == key.path }
            if viewed { screen.viewed.append(mark) }
            screen.error = nil
        } catch {
            screen.error = Self.message(error)
        }
    }

    public func comment(on key: FileKey, lines: [DiffLine], body: String) async {
        let text = body.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let anchor = CommentAnchor(lines: lines), !text.isEmpty else { return }
        let params = ReviewCommentParams(
            session: screen.session, worktree: key.worktree, path: key.path, startLine: anchor.start, endLine: anchor.end,
            code: anchor.code.joined(separator: "\n"), body: text, removed: anchor.removed
        )
        do {
            let draft: ReviewDraft = try await caller.call("review.comment", params: params)
            screen.draft = draft
            screen.composer = nil
            screen.error = nil
        } catch {
            screen.error = Self.message(error)
        }
    }

    public func send() async {
        guard screen.canSend else { return }
        do {
            let draft: ReviewDraft = try await caller.call(
                "review.send", params: ReviewSendParams(session: screen.session, note: screen.note.trimmingCharacters(in: .whitespacesAndNewlines))
            )
            screen.draft = draft
            screen.note = ""
            screen.error = nil
        } catch {
            screen.error = Self.message(error)
        }
    }

    public func hunk(_ key: FileKey, index: Int, action: HunkAction) async {
        guard let found = screen.file(key) else { return }
        let params = ReviewHunkParams(session: screen.session, worktree: key.worktree, file: found.file, hunk: index, action: action)
        do {
            let _: Empty = try await caller.call("review.hunk", params: params)
            screen.error = nil
        } catch {
            screen.error = Self.message(error)
            return
        }
        await open()
    }

    public func openInNvim(_ key: FileKey, line: Int) async -> Bool {
        do {
            let _: NvimOpened = try await caller.call(
                "nvim.open", params: NvimOpenParams(session: screen.session, worktree: key.worktree, path: key.path, line: line)
            )
            screen.error = nil
            return true
        } catch {
            screen.error = Self.message(error)
            return false
        }
    }
}
