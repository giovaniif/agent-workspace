#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

extension Color {
    init(hex: String) {
        let v = Int(hex.dropFirst(), radix: 16) ?? 0
        self.init(
            .sRGB,
            red: Double(v >> 16 & 0xff) / 255,
            green: Double(v >> 8 & 0xff) / 255,
            blue: Double(v & 0xff) / 255
        )
    }
}

struct Theme {
    let palette: Palette

    init(_ scheme: ColorScheme) {
        palette = scheme == .dark ? .mocha : .latte
    }

    func callAsFunction(_ tone: Tone) -> Color { Color(hex: palette.hex(tone)) }

    func token(_ cls: String?) -> Color {
        let latte = palette == .latte
        switch cls {
        case "keyword": return Color(hex: latte ? "#8839ef" : "#cba6f7")
        case "string": return Color(hex: latte ? "#40a02b" : "#a6e3a1")
        case "number": return Color(hex: latte ? "#fe640b" : "#fab387")
        case "comment": return Color(hex: latte ? "#8c8fa1" : "#7f849c")
        case "function": return Color(hex: latte ? "#1e66f5" : "#89b4fa")
        case "type": return Color(hex: latte ? "#df8e1d" : "#f9e2af")
        case "operator": return Color(hex: latte ? "#04a5e5" : "#89dceb")
        case "punctuation": return self(.subtext)
        default: return self(.text)
        }
    }
}

private struct AnimatesKey: EnvironmentKey {
    static let defaultValue = true
}

extension EnvironmentValues {
    public var agentwsAnimates: Bool {
        get { self[AnimatesKey.self] }
        set { self[AnimatesKey.self] = newValue }
    }
}

enum Metrics {
    static let sidebarWidth: CGFloat = 280
    static let railWidth: CGFloat = 52
    static let treeWidth: CGFloat = 250
    static let inspectorWidth: CGFloat = 320
    static let toolbarHeight: CGFloat = 46
    static let gutter: CGFloat = 12
    static let corner: CGFloat = 6
    static let mono = Font.system(size: 11, design: .monospaced)
    static let code = Font.system(size: 12, design: .monospaced)
}
#endif
