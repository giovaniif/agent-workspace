#if canImport(AppKit) && canImport(SwiftUI)
import CrewKit
import AppKit
import SwiftUI

@MainActor
public enum Snapshot {
    public static func render(_ scene: WindowScene, size: CGSize, dark: Bool) -> NSBitmapImageRep {
        renderView(MainWindow(scene: scene), size: size, dark: dark)
    }

    public static func render(disk scene: DiskScene, size: CGSize, dark: Bool) -> NSBitmapImageRep {
        renderView(DiskWindow(scene: scene), size: size, dark: dark)
    }

    public static func render(automations scene: AutomationsScene, size: CGSize, dark: Bool) -> NSBitmapImageRep {
        renderView(AutomationsWindow(scene: scene), size: size, dark: dark)
    }

    public static func render(mates scene: MatesScene, size: CGSize, dark: Bool) -> NSBitmapImageRep {
        renderView(MatesWindow(scene: scene), size: size, dark: dark)
    }

    public static func renderAutomationRun(_ automation: Automation, size: CGSize, dark: Bool) -> NSBitmapImageRep {
        renderView(AutomationRunSheet(automation: automation, run: { _, _ in }, cancel: {}), size: size, dark: dark)
    }

    public static func settings(_ scene: SettingsScene, size: CGSize, dark: Bool) -> NSBitmapImageRep {
        renderView(SettingsView(scene: scene), size: size, dark: dark)
    }

    public static func renderNewSession(_ model: NewSession, size: CGSize, dark: Bool) -> NSBitmapImageRep {
        renderView(NewSessionSheet(model: model, server: "This Mac", state: nil, live: false), size: size, dark: dark)
    }

    public static func renderFirstRun(_ flow: FirstRunFlow, hosts: [String], size: CGSize, dark: Bool) -> NSBitmapImageRep {
        let view = FirstRunView(
            appBuild: "v0.0.0", localBinary: "crew", bundled: { _ in nil }, hosts: hosts,
            cli: CLILink(target: "/Applications/crew.app/Contents/Resources/bin/crew"), initial: flow, cancel: {}, finish: { _ in }
        )
        return renderView(view, size: size, dark: dark)
    }

    public static func render(menu scene: MenuScene, size: CGSize, dark: Bool) -> NSBitmapImageRep {
        renderView(AttentionMenuView(scene: scene), size: size, dark: dark)
    }

    static func renderView(_ content: some View, size: CGSize, dark: Bool) -> NSBitmapImageRep {
        let root = content
            .environment(\.colorScheme, dark ? .dark : .light)
            .environment(\.crewAnimates, false)
            .frame(width: size.width, height: size.height)
        let view = NSHostingView(rootView: root)
        view.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        view.frame = CGRect(origin: .zero, size: size)
        let window = NSWindow(contentRect: view.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.contentView = view
        view.layoutSubtreeIfNeeded()
        RunLoop.main.run(until: Date().addingTimeInterval(0.1))
        view.layoutSubtreeIfNeeded()
        guard let rep = view.bitmapImageRepForCachingDisplay(in: view.bounds) else {
            preconditionFailure("no bitmap for a \(size) view")
        }
        view.cacheDisplay(in: view.bounds, to: rep)
        return rep
    }
}
#endif
