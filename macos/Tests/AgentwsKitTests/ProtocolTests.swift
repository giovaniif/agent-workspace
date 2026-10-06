import Testing
@testable import AgentwsKit

struct ProtocolTests {
    @Test func speaksTheDaemonsProtocolVersion() {
        #expect(AgentwsProtocol.version == 1)
    }
}
