import Testing
@testable import AgentwsKit

struct BackoffTests {
    @Test func startsAtOneSecondAndDoublesToThirty() {
        var backoff = Backoff()
        let delays = (0..<8).map { _ in backoff.next() }
        #expect(delays == [.seconds(1), .seconds(2), .seconds(4), .seconds(8), .seconds(16), .seconds(30), .seconds(30), .seconds(30)])
    }

    @Test func resetStartsOverAtOneSecond() {
        var backoff = Backoff()
        _ = backoff.next()
        _ = backoff.next()
        backoff.reset()
        #expect(backoff.next() == .seconds(1))
    }
}
