import Foundation

public enum Glyph: Sendable, Equatable {
    case filledDiamond
    case filledDot
    case spinningRing
    case hollowRing
}

public enum Tone: Sendable, Equatable, CaseIterable {
    case red
    case peach
    case blue
    case green
    case grey
    case text
    case subtext
    case surface
    case base
    case mantle
    case crust
}

public struct StateStyle: Sendable, Equatable {
    public var glyph: Glyph
    public var tone: Tone
    public var label: String
    public var symbol: String

    public init(state: String) {
        switch state {
        case "permission": self.init(.filledDiamond, .red, "Permission", "◆")
        case "waiting": self.init(.filledDot, .peach, "Waiting", "●")
        case "running": self.init(.spinningRing, .blue, "Running", "◌")
        case "done": self.init(.filledDot, .green, "Done", "✓")
        case "idle": self.init(.hollowRing, .grey, "Idle", "○")
        default: self.init(.hollowRing, .grey, state, "?")
        }
    }

    private init(_ glyph: Glyph, _ tone: Tone, _ label: String, _ symbol: String) {
        self.glyph = glyph
        self.tone = tone
        self.label = label
        self.symbol = symbol
    }

    public static func needsYou(_ state: String) -> Bool {
        state == "permission" || state == "waiting"
    }
}

public struct Palette: Sendable, Equatable {
    private let colors: [Tone: String]

    public func hex(_ tone: Tone) -> String { colors[tone] ?? "#000000" }

    public static let latte = Palette(colors: [
        .red: "#d20f39", .peach: "#fe640b", .blue: "#1e66f5", .green: "#40a02b", .grey: "#9ca0b0",
        .text: "#4c4f69", .subtext: "#6c6f85", .surface: "#ccd0da", .base: "#eff1f5", .mantle: "#e6e9ef", .crust: "#dce0e8",
    ])

    public static let mocha = Palette(colors: [
        .red: "#f38ba8", .peach: "#fab387", .blue: "#89b4fa", .green: "#a6e3a1", .grey: "#6c7086",
        .text: "#cdd6f4", .subtext: "#a6adc8", .surface: "#313244", .base: "#1e1e2e", .mantle: "#181825", .crust: "#11111b",
    ])
}
