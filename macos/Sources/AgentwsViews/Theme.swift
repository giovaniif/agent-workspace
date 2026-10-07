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
    static let inspectorWidth: CGFloat = 320
    static let toolbarHeight: CGFloat = 46
    static let gutter: CGFloat = 12
    static let corner: CGFloat = 6
    static let mono = Font.system(size: 11, design: .monospaced)
}
#endif
