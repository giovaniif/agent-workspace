import Testing
import AgentwsKit

struct NavigatorTests {
    let sidebar = Sidebar(state: Seed.state(sessions: [
        SeedSession(id: "a", name: "a", state: "running"),
        SeedSession(id: "b", name: "b", state: "waiting"),
        SeedSession(id: "c", name: "c", state: "idle"),
        SeedSession(id: "d", name: "d", state: "permission"),
    ]))

    @Test func commandDigitsJumpToThatRow() {
        var nav = Navigator()
        nav.jump(to: 3, in: sidebar)
        #expect(nav.selected == "c")
        nav.jump(to: 9, in: sidebar)
        #expect(nav.selected == "c")
    }

    @Test func controlSpaceCyclesThroughSessionsThatNeedYou() {
        var nav = Navigator()
        nav.select("a")
        nav.nextWaiting(in: sidebar)
        #expect(nav.selected == "b")
        nav.nextWaiting(in: sidebar)
        #expect(nav.selected == "d")
        nav.nextWaiting(in: sidebar)
        #expect(nav.selected == "b")
    }

    @Test func controlSpaceWithNothingSelectedStartsAtTheTop() {
        var nav = Navigator()
        nav.nextWaiting(in: sidebar)
        #expect(nav.selected == "b")
    }

    @Test func controlSpaceWithNobodyWaitingStays() {
        let calm = Sidebar(state: Seed.state(sessions: [SeedSession(id: "a", name: "a", state: "idle")]))
        var nav = Navigator()
        nav.select("a")
        nav.nextWaiting(in: calm)
        #expect(nav.selected == "a")
    }

    @Test func commandBracketGoesBackToTheLastSession() {
        var nav = Navigator()
        nav.select("a")
        nav.select("c")
        nav.select("c")
        nav.last()
        #expect(nav.selected == "a")
        nav.last()
        #expect(nav.selected == "c")
    }

    @Test func aRemovedSelectionFallsBackToTheFirstRow() {
        var nav = Navigator()
        nav.select("gone")
        nav.reconcile(with: sidebar)
        #expect(nav.selected == "a")
        nav.select("c")
        nav.reconcile(with: sidebar)
        #expect(nav.selected == "c")
    }
}
