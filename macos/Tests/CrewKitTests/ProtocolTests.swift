import Testing
@testable import CrewKit

struct ProtocolTests {
    @Test func speaksTheDaemonsProtocolVersion() {
        #expect(CrewProtocol.version == 1)
    }
}
