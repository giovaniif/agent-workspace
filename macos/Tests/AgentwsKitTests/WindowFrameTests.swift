import Foundation
import Testing
@testable import AgentwsKit

struct WindowFrameTests {
    @Test func theFirstMainWindowTakesEightyPercentOfTheScreenCentered() {
        let frame = WindowFrame.initial(visible: CGRect(x: 0, y: 0, width: 2000, height: 1250))
        #expect(frame == CGRect(x: 200, y: 125, width: 1600, height: 1000))
    }

    @Test func aSmallScreenStillGetsTheMinimumSize() {
        let frame = WindowFrame.initial(visible: CGRect(x: 0, y: 25, width: 1440, height: 875))
        #expect(frame == CGRect(x: 80, y: 62.5, width: 1280, height: 800))
    }

    @Test func theFrameNeverGrowsPastTheVisibleScreen() {
        let frame = WindowFrame.initial(visible: CGRect(x: -1024, y: 40, width: 1024, height: 700))
        #expect(frame == CGRect(x: -1024, y: 40, width: 1024, height: 700))
    }
}
