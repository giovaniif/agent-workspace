import Foundation
import Testing
@testable import AgentwsKit

struct FakeServer {
    let bin: FakeBin
    let home: String
    let bundle: String

    init(uname: (kernel: String, machine: String) = ("Linux", "aarch64"), password: Bool = false) throws {
        bin = try FakeBin()
        home = bin.path("remote-home")
        bundle = bin.path("bundle")
        try FileManager.default.createDirectory(atPath: home, withIntermediateDirectories: true)
        try FileManager.default.createDirectory(atPath: bundle + "/linux_arm64", withIntermediateDirectories: true)
        let login = password
            ? "echo 'box: Permission denied (publickey,password).' >&2\nexit 255"
            : "for last; do :; done\nHOME=\"\(home)\" exec sh -c \"$last\""
        try bin.script("ssh", """
        printf '%s %s %s %s %s %s\\n' "$1" "$2" "$3" "$4" "$5" "$6" >> "\(bin.path("ssh.log"))"
        case " $* " in
          *" BatchMode=yes "*) ;;
          *) read -r password < /dev/tty; exec sleep 600 ;;
        esac
        \(login)
        """)
        try bin.script("uname", """
        case "$1" in
          -s) echo \(uname.kernel) ;;
          -m) echo \(uname.machine) ;;
        esac
        """)
        try bin.script("systemctl", "printf '%s\\n' \"$*\" >> \"\(bin.path("systemctl.log"))\"\n")
        let built = bundle + "/linux_arm64/agentws"
        try agentws(build: "v1.2.0 abc").write(toFile: built, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: built)
    }

    func agentws(build: String) -> String {
        """
        #!/bin/sh
        case "$*" in
          "version --build") echo "\(build)" ;;
          "setup daemon --check")
            if [ -f "\(bin.path("unit"))" ]; then echo '{"installed":true,"running":true,"linger":false}'
            else echo '{"installed":false,"running":false,"linger":false}'; fi ;;
          "setup daemon")
            echo "setup daemon" >> "\(bin.path("daemon.log"))"
            if [ -f "\(bin.path("unit"))" ]; then echo "already set up"; else touch "\(bin.path("unit"))"; echo "wrote and started"; fi ;;
          *) echo "unexpected $*" >> "\(bin.path("daemon.log"))"; exit 2 ;;
        esac
        """
    }

    func installOld(build: String) throws {
        let dir = home + "/.local/bin"
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try agentws(build: build).write(toFile: dir + "/agentws", atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: dir + "/agentws")
        try bin.write("unit", "")
    }

    var remoteBinary: String { home + "/.local/bin/agentws" }

    var remoteBuild: String? {
        guard let text = try? String(contentsOfFile: remoteBinary, encoding: .utf8) else { return nil }
        return text.contains("v1.2.0 abc") ? "v1.2.0 abc" : "other"
    }

    var inode: Int? {
        (try? FileManager.default.attributesOfItem(atPath: remoteBinary))?[.systemFileNumber] as? Int
    }

    @MainActor
    func setup() -> ServerSetup {
        let bundle = bundle
        return ServerSetup(
            server: .ssh(host: "box"),
            appBuild: "v1.2.0 abc",
            bundled: { platform in "\(bundle)/\(platform.directory)/agentws" },
            shell: ProcessShell(server: .ssh(host: "box"), environment: bin.environment)
        )
    }
}

@MainActor
struct ServersTests {
    @Test func sshConfigListsConcreteHostsOnly() {
        let config = """
        Host *
          ServerAliveInterval 30
        Host box devbox
          HostName 100.64.0.2
        host  build-?? !jump
        Match host foo
        Host box
        """
        #expect(SSHConfig.hosts(config) == ["box", "devbox"])
    }

    @Test func aTypedTargetMustBeOneSshDestination() {
        #expect(SSHConfig.target("  me@box ") == "me@box")
        #expect(SSHConfig.target("box") == "box")
        #expect(SSHConfig.target("-oProxyCommand=sh") == nil)
        #expect(SSHConfig.target("me@box ls") == nil)
        #expect(SSHConfig.target("") == nil)
    }

    @Test func unameMapsToTheBundledBinaryDirectory() {
        #expect(Platform(kernel: "Linux", machine: "x86_64")?.directory == "linux_amd64")
        #expect(Platform(kernel: "Linux", machine: "aarch64")?.directory == "linux_arm64")
        #expect(Platform(kernel: "Darwin", machine: "arm64")?.directory == "darwin_arm64")
        #expect(Platform(kernel: "Darwin", machine: "x86_64")?.directory == "darwin_amd64")
        #expect(Platform(kernel: "FreeBSD", machine: "amd64") == nil)
        #expect(Platform(kernel: "Linux", machine: "armv7l") == nil)
    }

    @Test func aProbeReportsMissingToolsAndGhSignIn() {
        let probe = Probe.parse("""
        kernel=Linux
        machine=x86_64
        user=me
        tool=git
        tool=gh
        """)
        let items = Dictionary(uniqueKeysWithValues: Checklist.items(server: .ssh(host: "box"), probe: probe, appBuild: "v1").map { ($0.id, $0) })
        #expect(items["git"]?.state == .ok)
        #expect(items["tmux"]?.state == .failed)
        #expect(items["gh"]?.state == .warning)
        #expect(items["gh"]?.command == "ssh -t box gh auth login")
        #expect(items["agentws"]?.state == .failed)
        #expect(items["agentws"]?.fix == "Set up")
    }

    @Test func aServerOnAnotherBuildOffersTheUpdate() {
        let probe = Probe.parse("kernel=Linux\nmachine=x86_64\nuser=me\nbuild=v1.1.0 old\ndaemon={\"installed\":true,\"running\":true,\"linger\":true}\n")
        let item = Checklist.items(server: .ssh(host: "box"), probe: probe, appBuild: "v1.2.0 abc").first { $0.id == "agentws" }
        #expect(item?.state == .warning)
        #expect(item?.fix == "Update server")
    }

    @Test(.timeLimit(.minutes(1))) func aFreshLinuxServerGetsTheBundledBuildAndADaemon() async throws {
        let server = try FakeServer()
        let setup = server.setup()
        await setup.setUp()
        #expect(setup.error == nil)
        #expect(server.remoteBuild == "v1.2.0 abc")
        #expect(server.bin.read("daemon.log") == "setup daemon\n")
        #expect(setup.probe?.platform == Platform(kernel: "Linux", machine: "aarch64"))
        #expect(setup.probe?.build == "v1.2.0 abc")
        #expect(setup.probe?.daemon?.running == true)
        #expect(setup.lingerCommand == "sudo loginctl enable-linger \(NSUserName())")
        #expect(server.bin.read("ssh.log").split(separator: "\n").allSatisfy { $0 == "-T -o BatchMode=yes -o ConnectTimeout=10 box" })
        #expect(server.bin.read("systemctl.log") == "")
    }

    @Test(.timeLimit(.minutes(1))) func reRunningSetupOnAConfiguredServerChangesNothing() async throws {
        let server = try FakeServer()
        let setup = server.setup()
        await setup.setUp()
        let inode = server.inode
        await setup.setUp()
        #expect(setup.error == nil)
        #expect(server.inode == inode)
        #expect(server.bin.read("daemon.log") == "setup daemon\nsetup daemon\n")
        #expect(server.bin.read("systemctl.log") == "")
    }

    @Test(.timeLimit(.minutes(1))) func anOutdatedServerIsUpdatedByRestartingTheDaemonNotStoppingIt() async throws {
        let server = try FakeServer()
        try server.installOld(build: "v1.1.0 old")
        let setup = server.setup()
        await setup.setUp()
        #expect(setup.error == nil)
        #expect(server.remoteBuild == "v1.2.0 abc")
        #expect(server.bin.read("systemctl.log") == "--user restart agentws-daemon.service\n")
        #expect(!server.bin.read("daemon.log").contains("--remove"))
    }

    @Test(.timeLimit(.minutes(1))) func aHostThatNeedsAPasswordFailsFastAboutKeyOrAgentLogin() async throws {
        let server = try FakeServer(password: true)
        let setup = server.setup()
        let start = ContinuousClock.now
        await setup.setUp()
        #expect(ContinuousClock.now - start < .seconds(10))
        let error = try #require(setup.error)
        #expect(error.contains("key"))
        #expect(error.contains("agent"))
        #expect(error.contains("Permission denied"))
        #expect(server.inode == nil)
    }

    @Test(.timeLimit(.minutes(1))) func anUnsupportedArchitectureInstallsNothing() async throws {
        let server = try FakeServer(uname: ("Linux", "armv7l"))
        let setup = server.setup()
        await setup.setUp()
        #expect(setup.error?.contains("armv7l") == true)
        #expect(server.inode == nil)
    }

    @Test func thisMacSkipsTheHostStep() {
        var flow = FirstRunFlow()
        flow.next()
        #expect(flow.step == .location)
        flow.kind = .thisMac
        flow.next()
        #expect(flow.step == .setup)
        flow.back()
        #expect(flow.step == .location)
        flow.kind = .ssh(host: "box")
        flow.next()
        #expect(flow.step == .host)
        flow.next()
        #expect(flow.step == .setup)
        flow.next()
        flow.next()
        #expect(flow.step == .done)
        flow.next()
        #expect(flow.step == .done)
    }

    @Test func savedServersKeepThisMacFirstAndAddEachHostOnce() throws {
        let suite = "agentws-servers-\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        let list = ServerList(defaults: defaults)
        #expect(list.servers.isEmpty)
        list.add(.ssh(host: "box"))
        list.add(.thisMac)
        list.add(.ssh(host: "box"))
        #expect(list.servers == [.thisMac, .ssh(host: "box")])
        list.selected = .ssh(host: "box")
        let reloaded = ServerList(defaults: defaults)
        #expect(reloaded.servers == [.thisMac, .ssh(host: "box")])
        #expect(reloaded.selected == .ssh(host: "box"))
        reloaded.remove(.ssh(host: "box"))
        #expect(reloaded.selected == .thisMac)
        defaults.removePersistentDomain(forName: suite)
    }

    @Test func eachServerHasItsEndpoint() {
        #expect(ServerKind.ssh(host: "box").endpoint(localBinary: "/a/agentws") == .ssh(host: "box", remoteBinary: "~/.local/bin/agentws"))
        #expect(ServerKind.thisMac.endpoint(localBinary: "/a/agentws") == .local(binary: "/a/agentws"))
        #expect(ServerKind.ssh(host: "box").name == "box")
        #expect(ServerKind.thisMac.name == "This Mac")
    }
}
