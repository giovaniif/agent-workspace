import Foundation
import Testing
@testable import AgentwsKit

struct FakeBin {
    let dir: URL

    init() throws {
        dir = FileManager.default.temporaryDirectory.appendingPathComponent("agentws-kit-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    }

    @discardableResult
    func script(_ name: String, _ body: String) throws -> String {
        let url = dir.appendingPathComponent(name)
        try ("#!/bin/sh\n" + body).write(to: url, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
        return url.path
    }

    func path(_ name: String) -> String { dir.appendingPathComponent(name).path }

    func read(_ name: String) -> String {
        (try? String(contentsOfFile: path(name), encoding: .utf8)) ?? ""
    }

    var environment: [String: String] {
        var env = ProcessInfo.processInfo.environment
        env["PATH"] = dir.path + ":" + (env["PATH"] ?? "/usr/bin:/bin")
        return env
    }

    func write(_ name: String, _ text: String) throws {
        try text.write(toFile: path(name), atomically: true, encoding: .utf8)
    }
}

let echoReplies = #"""
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed 's/.*"id":\([0-9]*\).*/\1/')
  printf '{"v":1,"id":%s,"result":{"echo":%s}}\n' "$id" "$id"
done
"""#

struct Echo: Decodable, Equatable { let echo: Int }

struct TransportTests {
    @Test func sshRunsTheRemoteBridgeInBatchModeWithKeepalives() {
        let argv = Endpoint.ssh(host: "box", remoteBinary: "~/.local/bin/agentws").argv
        #expect(argv == ["ssh", "-T", "-o", "BatchMode=yes", "-o", "RemoteCommand=none", "-o", "RequestTTY=no", "-o", "ServerAliveInterval=15", "box", "~/.local/bin/agentws", "rpc"])
    }

    @Test func localRunsTheBundledBridge() {
        #expect(Endpoint.local(binary: "/Applications/agentws.app/Contents/MacOS/agentws").argv == ["/Applications/agentws.app/Contents/MacOS/agentws", "rpc"])
    }

    @Test(.timeLimit(.minutes(1))) func aFakeSshOnPathGetsTheArgvAndNeverPrompts() async throws {
        let bin = try FakeBin()
        try bin.script("ssh", """
        printf '%s\\n' "$@" > "\(bin.path("argv"))"
        case " $* " in
          *" BatchMode=yes "*) ;;
          *) read -r password < /dev/tty; exec sleep 600 ;;
        esac
        case " $* " in
          *" RemoteCommand=none "*) ;;
          *) echo 'Cannot execute command-line and remote command.' >&2; exit 255 ;;
        esac
        \(echoReplies)
        """)
        let client = try RPCClient(endpoint: .ssh(host: "box", remoteBinary: "agentws"), build: "test", environment: bin.environment)
        let reply: Echo = try await client.call("status", params: [String: String]())
        #expect(reply == Echo(echo: 1))
        #expect(bin.read("argv") == "-T\n-o\nBatchMode=yes\n-o\nRemoteCommand=none\n-o\nRequestTTY=no\n-o\nServerAliveInterval=15\nbox\nagentws\nrpc\n")
        client.close()
    }

    @Test(.timeLimit(.minutes(1))) func callsOnOneConnectionGetTheirOwnReplies() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", echoReplies)
        let client = try RPCClient(endpoint: .local(binary: agentws), build: "test", environment: bin.environment)
        async let a: Echo = client.call("status", params: [String: String]())
        async let b: Echo = client.call("status", params: [String: String]())
        let replies = try await [a.echo, b.echo].sorted()
        #expect(replies == [1, 2])
        client.close()
    }

    @Test(.timeLimit(.minutes(1))) func requestsCarryTheBuild() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", """
        IFS= read -r line
        printf '%s' "$line" > "\(bin.path("request"))"
        printf '{"v":1,"id":1,"result":{"echo":1}}\\n'
        cat > /dev/null
        """)
        let client = try RPCClient(endpoint: .local(binary: agentws), build: "v9.9.9", environment: bin.environment)
        let _: Echo = try await client.call("session.mute", params: ["id": "s1"])
        let request = try #require(try JSONSerialization.jsonObject(with: Data(bin.read("request").utf8)) as? [String: Any])
        #expect(request["build"] as? String == "v9.9.9")
        #expect(request["method"] as? String == "session.mute")
        client.close()
    }

    @Test(.timeLimit(.minutes(1))) func noDaemonIsATypedUnavailableError() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", """
        printf '{"v":1,"id":0,"error":{"code":"unavailable","message":"no agentws daemon is running"}}\\n'
        exit 1
        """)
        let client = try RPCClient(endpoint: .local(binary: agentws), build: "test", environment: bin.environment)
        await #expect(throws: AgentwsError.rpc(RPCError(code: "unavailable", message: "no agentws daemon is running"))) {
            let _: Echo = try await client.call("status", params: [String: String]())
        }
    }

    @Test(.timeLimit(.minutes(1))) func aBuildMismatchIsATypedError() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", """
        IFS= read -r line
        printf '{"v":1,"id":1,"error":{"code":"version_mismatch","message":"restart the daemon"}}\\n'
        cat > /dev/null
        """)
        let client = try RPCClient(endpoint: .local(binary: agentws), build: "test", environment: bin.environment)
        do {
            let _: Echo = try await client.call("status", params: [String: String]())
            Issue.record("expected an error")
        } catch let AgentwsError.rpc(error) {
            #expect(error.kind == .versionMismatch)
        }
        client.close()
    }

    @Test(.timeLimit(.minutes(1))) func aDeadProcessFailsPendingCalls() async throws {
        let bin = try FakeBin()
        let agentws = try bin.script("agentws", "IFS= read -r line\nexit 0\n")
        let client = try RPCClient(endpoint: .local(binary: agentws), build: "test", environment: bin.environment)
        await #expect(throws: AgentwsError.disconnected) {
            let _: Echo = try await client.call("status", params: [String: String]())
        }
    }
}
