#if canImport(AppKit) && canImport(SwiftUI)
import AgentwsKit
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

    static func renderView(_ content: some View, size: CGSize, dark: Bool) -> NSBitmapImageRep {
        let root = content
            .environment(\.colorScheme, dark ? .dark : .light)
            .environment(\.agentwsAnimates, false)
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
