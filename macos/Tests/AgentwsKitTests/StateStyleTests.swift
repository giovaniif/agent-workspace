import Testing
import AgentwsKit

struct StateStyleTests {
    @Test func everyStateHasItsOwnShapeAndColour() {
        let expected: [(String, Glyph, Tone)] = [
            ("permission", .filledDiamond, .red),
            ("waiting", .filledDot, .peach),
            ("running", .spinningRing, .blue),
            ("done", .filledDot, .green),
            ("idle", .hollowRing, .grey),
        ]
        for (state, glyph, tone) in expected {
            let style = StateStyle(state: state)
            #expect(style.glyph == glyph, "\(state)")
            #expect(style.tone == tone, "\(state)")
        }
    }

    @Test func statesThatShareAShapeAreNamed() {
        let states = ["permission", "waiting", "running", "done", "idle"].map { StateStyle(state: $0) }
        #expect(states.map(\.label) == ["Permission", "Waiting", "Running", "Done", "Idle"])
        #expect(Set(states.map(\.symbol)).count == states.count)
    }

    @Test func anUnknownStateIsAHollowGreyRingNamedAsSent() {
        let style = StateStyle(state: "compacting")
        #expect(style.glyph == .hollowRing)
        #expect(style.tone == .grey)
        #expect(style.label == "compacting")
    }

    @Test func tonesUseCatppuccinLatteAndMocha() {
        #expect(Palette.latte.hex(.red) == "#d20f39")
        #expect(Palette.latte.hex(.peach) == "#fe640b")
        #expect(Palette.latte.hex(.blue) == "#1e66f5")
        #expect(Palette.latte.hex(.green) == "#40a02b")
        #expect(Palette.latte.hex(.grey) == "#9ca0b0")
        #expect(Palette.mocha.hex(.red) == "#f38ba8")
        #expect(Palette.mocha.hex(.peach) == "#fab387")
        #expect(Palette.mocha.hex(.blue) == "#89b4fa")
        #expect(Palette.mocha.hex(.green) == "#a6e3a1")
        #expect(Palette.mocha.hex(.grey) == "#6c7086")
    }
}
