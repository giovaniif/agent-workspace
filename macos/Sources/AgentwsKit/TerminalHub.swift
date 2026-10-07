import Foundation
import Observation

public enum TerminalStatus: Equatable, Sendable {
    case idle
    case connecting
    case live
    case reconnecting(String)
}

@MainActor
@Observable
public final class TerminalHub {
    public typealias Sink = @MainActor ([UInt8]) -> Void
    public typealias Open = @Sendable (Int, Int) async throws -> NativeClient

    private enum Held {
        case bytes([UInt8])
        case reply(Int)
    }

    private struct Draw {
        var held: [Held] = []
        var until: Int?
    }

    public private(set) var status: TerminalStatus = .idle
    public private(set) var panes = TerminalPanes()
    @ObservationIgnored public var size = (cols: 200, rows: 50)

    private let endpoint: Endpoint
    private let environment: [String: String]?
    private let backoff: Backoff
    private let open: Open
    @ObservationIgnored private var sinks: [String: Sink] = [:]
    @ObservationIgnored private var live: Set<String> = []
    @ObservationIgnored private var draws: [String: Draw] = [:]
    @ObservationIgnored private var link: TerminalLink?
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var sized: [String: String] = [:]

    public init(endpoint: Endpoint, environment: [String: String]? = nil, backoff: Backoff = Backoff(), open: @escaping Open) {
        self.endpoint = endpoint
        self.environment = environment
        self.backoff = backoff
        self.open = open
    }

    public func start() {
        guard task == nil else { return }
        status = .connecting
        task = Task { await run() }
    }

    public func stop() {
        task?.cancel()
        task = nil
        link?.close()
        link = nil
        status = .idle
    }

    public func attach(pane: String, sink: @escaping Sink) {
        sinks[pane] = sink
        panes.show(pane)
        live.remove(pane)
        draw(pane)
    }

    public func show(pane: String) {
        if panes.show(pane) == .resumeAndRedraw { redraw(pane) }
    }

    public func hide(pane: String) {
        panes.hide(pane)
    }

    public func detach(pane: String) {
        panes.hide(pane)
        sinks[pane] = nil
        live.remove(pane)
        draws[pane] = nil
    }

    public func send(pane: String, bytes: [UInt8]) {
        guard let link else { return }
        for line in ControlCommand.sendKeys(pane: pane, bytes: bytes) { link.post(line) }
    }

    public func resize(pane: String, cols: Int, rows: Int) {
        size = (cols, rows)
        guard let link, let window = panes[pane]?.window else { return }
        let command = ControlCommand.size(window: window, cols: cols, rows: rows)
        guard sized[window] != command else { return }
        sized[window] = command
        link.post(command)
    }

    public func letterbox(pane: String, viewCols: Int, viewRows: Int) -> Letterbox? {
        guard let state = panes[pane] else { return nil }
        return Letterbox(paneCols: state.cols, paneRows: state.rows, viewCols: viewCols, viewRows: viewRows)
    }

    private func run() async {
        var backoff = backoff
        while !Task.isCancelled {
            let native: NativeClient
            let link: TerminalLink
            do {
                native = try await open(size.cols, size.rows)
                link = try TerminalLink(argv: endpoint.terminalArgv(native.argv), environment: environment)
            } catch {
                if Task.isCancelled { return }
                status = .reconnecting(String(describing: error))
                try? await Task.sleep(for: backoff.next())
                continue
            }
            if Task.isCancelled {
                link.close()
                return
            }
            self.link = link
            for pane in native.panes { panes.set(pane) }
            panes.reset()
            live = []
            draws = [:]
            sized = [:]
            status = .live
            backoff.reset()
            link.post(ControlCommand.pauseAfter(seconds: 5))
            for pane in sinks.keys.sorted() { draw(pane) }
            for await event in link.events { handle(event) }
            link.close()
            if self.link === link { self.link = nil }
            if Task.isCancelled { return }
            status = .reconnecting("connection lost")
            try? await Task.sleep(for: backoff.next())
        }
    }

    private func handle(_ event: ControlEvent) {
        switch event {
        case let .output(pane, bytes):
            if live.contains(pane) {
                sinks[pane]?(bytes)
            } else if var d = draws[pane], d.until == nil {
                d.held.append(.bytes(bytes))
                draws[pane] = d
            }
        case let .reply(number, _, _):
            for (pane, d) in draws {
                if d.until == number {
                    draws[pane] = nil
                    live.insert(pane)
                } else if d.until == nil {
                    draws[pane]?.held.append(.reply(number))
                }
            }
        case .layout, .resume:
            panes.apply(event)
        case .windowClose:
            for pane in panes.apply(event) {
                live.remove(pane)
                draws[pane] = nil
            }
        case let .pause(pane):
            if panes.pause(pane) == .resumeAndRedraw { redraw(pane) }
        case .exit:
            link?.close()
        case .begin, .windowAdd:
            break
        }
    }

    private func redraw(_ pane: String) {
        guard let link else { return }
        link.post(ControlCommand.resume(pane: pane))
        live.remove(pane)
        draw(pane)
    }

    private func draw(_ pane: String) {
        guard let link, sinks[pane] != nil else { return }
        draws[pane] = Draw()
        Task {
            let query = try? await link.command(ControlCommand.query(pane: pane))
            guard self.link === link else { return }
            if let line = query?.lines.first, let state = PaneState(query: line) { panes.set(state) }
            guard let capture = try? await link.command(ControlCommand.capture(pane: pane)), capture.ok else {
                if self.link === link, draws[pane] != nil {
                    draws[pane] = nil
                    live.insert(pane)
                }
                return
            }
            guard self.link === link, let sink = sinks[pane], var d = draws[pane], d.until == nil else { return }
            let state = panes[pane] ?? PaneState(pane: pane, window: "", cols: size.cols, rows: size.rows)
            sink(state.prime(history: capture.lines))
            if let mark = d.held.firstIndex(where: { if case .reply(capture.number) = $0 { true } else { false } }) {
                for case let .bytes(bytes) in d.held[(mark + 1)...] { sink(bytes) }
                draws[pane] = nil
                live.insert(pane)
            } else {
                d.held = []
                d.until = capture.number
                draws[pane] = d
            }
        }
    }
}
