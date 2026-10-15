import Foundation
#if canImport(CoreGraphics)
import CoreGraphics
#endif

public enum WindowFrame {
    public static let autosaveName = "crew.main"
    public static let minimum = CGSize(width: 1280, height: 800)
    public static let share: CGFloat = 0.8

    public static func initial(visible: CGRect) -> CGRect {
        let width = min(max(visible.width * share, minimum.width), visible.width)
        let height = min(max(visible.height * share, minimum.height), visible.height)
        return CGRect(x: visible.midX - width / 2, y: visible.midY - height / 2, width: width, height: height)
    }
}
