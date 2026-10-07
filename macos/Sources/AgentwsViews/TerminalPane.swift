#if canImport(SwiftUI) && canImport(AppKit) && canImport(SwiftTerm)
import AgentwsKit
import AppKit
import SwiftTerm
import SwiftUI

@MainActor
public final class TerminalViews {
    private var hosts: [String: PaneHost] = [:]
    public var bound = Shortcuts.defaults.bound
    private var wantsFocus = false

    public init() {}

    public func focusNextShown() {
        wantsFocus = true
    }

    func claimFocus(_ host: PaneHost) {
        guard wantsFocus else { return }
        wantsFocus = false
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { [weak host] in
            guard let host else { return }
            host.window?.makeFirstResponder(host.terminal)
        }
    }

    func host(_ pane: String, hub: TerminalHub) -> PaneHost {
        if let host = hosts[pane], host.hub === hub { return host }
        let host = PaneHost(pane: pane, hub: hub)
        host.terminal.bound = { [weak self] in self?.bound ?? Shortcuts.defaults.bound }
        hosts[pane] = host
        return host
    }

    func forget(except panes: Set<String>, hub: TerminalHub) {
        for (pane, host) in hosts where !panes.contains(pane) && host.superview == nil {
            hub.detach(pane: pane)
            hosts[pane] = nil
        }
    }
}

final class PaneTerminal: TerminalView {
    var bound: () -> Set<KeyCombo> = { Shortcuts.defaults.bound }

    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        var modifiers: Modifiers = []
        if flags.contains(.command) { modifiers.insert(.command) }
        if flags.contains(.control) { modifiers.insert(.control) }
        if flags.contains(.option) { modifiers.insert(.option) }
        if flags.contains(.shift) { modifiers.insert(.shift) }
        let forwards = TerminalKeys.goesToPane(
            key: event.charactersIgnoringModifiers ?? "", modifiers: modifiers, bound: bound()
        )
        if forwards, window?.firstResponder === self {
            keyDown(with: event)
            return true
        }
        return super.performKeyEquivalent(with: event)
    }
}

@MainActor
final class PaneHost: NSView, @preconcurrency TerminalViewDelegate {
    let pane: String
    let hub: TerminalHub
    let terminal = PaneTerminal(frame: .zero)
    private let note = NSTextField(labelWithString: "")
    private var boxed: Letterbox?

    init(pane: String, hub: TerminalHub) {
        self.pane = pane
        self.hub = hub
        super.init(frame: .zero)
        terminal.font = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
        terminal.terminalDelegate = self
        terminal.optionAsMetaKey = true
        addSubview(terminal)
        note.font = NSFont.systemFont(ofSize: 11)
        note.isHidden = true
        addSubview(note)
        applyColors()
        hub.attach(pane: pane) { [weak self] bytes in
            self?.terminal.feed(byteArray: bytes[...])
        }
    }

    required init?(coder: NSCoder) { nil }

    override func viewDidChangeEffectiveAppearance() {
        super.viewDidChangeEffectiveAppearance()
        applyColors()
    }

    private func applyColors() {
        let dark = effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
        let palette: Palette = dark ? .mocha : .latte
        terminal.nativeBackgroundColor = NSColor(hex: palette.hex(.base))
        terminal.nativeForegroundColor = NSColor(hex: palette.hex(.text))
        note.textColor = NSColor(hex: palette.hex(.subtext))
        wantsLayer = true
        layer?.backgroundColor = NSColor(hex: palette.hex(.mantle)).cgColor
    }

    private var grid: (cols: Int, rows: Int, cell: NSSize) {
        let t = terminal.getTerminal()
        let optimal = terminal.getOptimalFrameSize().size
        let cell = NSSize(width: optimal.width / CGFloat(max(t.cols, 1)), height: optimal.height / CGFloat(max(t.rows, 1)))
        guard cell.width > 0, cell.height > 0 else { return (t.cols, t.rows, cell) }
        return (max(Int(bounds.width / cell.width), 2), max(Int((bounds.height - 22) / cell.height), 2), cell)
    }

    override func layout() {
        super.layout()
        let fit = grid
        boxed = hub.letterbox(pane: pane, viewCols: fit.cols, viewRows: fit.rows)
        if let boxed {
            let t = terminal.getTerminal()
            if t.cols != boxed.cols || t.rows != boxed.rows { terminal.resize(cols: boxed.cols, rows: boxed.rows) }
            let size = terminal.getOptimalFrameSize().size
            terminal.frame = NSRect(
                x: max((bounds.width - size.width) / 2, 0), y: max((bounds.height - size.height) / 2, 22),
                width: min(size.width, bounds.width), height: min(size.height, bounds.height - 22)
            )
            note.stringValue = boxed.note
            note.isHidden = false
            note.sizeToFit()
            note.frame.origin = NSPoint(x: (bounds.width - note.frame.width) / 2, y: 3)
        } else {
            note.isHidden = true
            terminal.frame = bounds
            hub.resize(pane: pane, cols: terminal.getTerminal().cols, rows: terminal.getTerminal().rows)
        }
    }

    func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
        if boxed == nil { hub.resize(pane: pane, cols: newCols, rows: newRows) }
    }

    func send(source: TerminalView, data: ArraySlice<UInt8>) {
        hub.send(pane: pane, bytes: Array(data))
    }

    func setTerminalTitle(source: TerminalView, title: String) {}
    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}
    func scrolled(source: TerminalView, position: Double) {}
    func requestOpenLink(source: TerminalView, link: String, params: [String: String]) {
        guard let url = URL(string: link), ["http", "https"].contains(url.scheme?.lowercased() ?? "") else { return }
        NSWorkspace.shared.open(url)
    }
    func bell(source: TerminalView) {}
    func clipboardCopy(source: TerminalView, content: Data) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(String(decoding: content, as: UTF8.self), forType: .string)
    }
    func iTermContent(source: TerminalView, content: ArraySlice<UInt8>) {}
    func rangeChanged(source: TerminalView, startY: Int, endY: Int) {}
}

extension NSColor {
    convenience init(hex: String) {
        let v = Int(hex.dropFirst(), radix: 16) ?? 0
        self.init(srgbRed: CGFloat(v >> 16 & 0xff) / 255, green: CGFloat(v >> 8 & 0xff) / 255, blue: CGFloat(v & 0xff) / 255, alpha: 1)
    }
}

final class PaneSlot: NSView {
    var current: PaneHost?
}

struct TerminalPaneView: NSViewRepresentable {
    let hub: TerminalHub
    let views: TerminalViews
    let pane: String
    let state: PaneState?

    func makeNSView(context: Context) -> PaneSlot { PaneSlot() }

    func updateNSView(_ slot: PaneSlot, context: Context) {
        let host = views.host(pane, hub: hub)
        if slot.current !== host {
            if let old = slot.current {
                hub.hide(pane: old.pane)
                old.removeFromSuperview()
            }
            host.frame = slot.bounds
            host.autoresizingMask = [.width, .height]
            slot.addSubview(host)
            slot.current = host
            hub.show(pane: pane)
            views.claimFocus(host)
        }
        host.needsLayout = true
        DispatchQueue.main.async { host.window?.makeFirstResponder(host.terminal) }
    }

    static func dismantleNSView(_ slot: PaneSlot, coordinator: ()) {
        MainActor.assumeIsolated {
            if let old = slot.current { old.hub.hide(pane: old.pane) }
        }
    }
}

struct TerminalArea: View {
    let hub: TerminalHub
    let views: TerminalViews
    let pane: String
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        ZStack(alignment: .top) {
            TerminalPaneView(hub: hub, views: views, pane: pane, state: hub.panes[pane])
            if case let .reconnecting(reason) = hub.status {
                Text("Reconnecting terminals… " + reason)
                    .font(.system(size: 12))
                    .foregroundStyle(theme(.subtext))
                    .padding(.horizontal, 10)
                    .padding(.vertical, 5)
                    .background(Capsule().fill(theme(.mantle)))
                    .padding(.top, 8)
            }
        }
    }
}

private struct TerminalsKey: EnvironmentKey {
    static let defaultValue: (hub: TerminalHub, views: TerminalViews)? = nil
}

extension EnvironmentValues {
    public var agentwsTerminals: (hub: TerminalHub, views: TerminalViews)? {
        get { self[TerminalsKey.self] }
        set { self[TerminalsKey.self] = newValue }
    }
}
#endif
