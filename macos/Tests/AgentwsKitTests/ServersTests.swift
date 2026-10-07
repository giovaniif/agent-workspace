import Foundation
import Testing
@testable import AgentwsKit

struct FakeServer {
    let bin: FakeBin
    let home: String
    let bundle: String

    init(uname: (kernel: String, machine: String) = ("Linux", "aarch64"), password: Bool = false, unreachable: Bool = false) throws {
        bin = try FakeBin()
        home = bin.path("remote-home")
        bundle = bin.path("bundle")
        try FileManager.default.createDirectory(atPath: home, withIntermediateDirectories: true)
        try FileManager.default.createDirectory(atPath: bundle + "/linux_arm64", withIntermediateDirectories: true)
        let login = password
            ? "echo 'box: Permission denied (publickey,password).' >&2\nexit 255"
            : unreachable
            ? "echo 'ssh: connect to host box port 22: Connection refused' >&2\nexit 255"
            : "for last; do :; done\nHOME=\"\(home)\" exec env -u XDG_RUNTIME_DIR -u DBUS_SESSION_BUS_ADDRESS sh -c \"$last\""
        try bin.script("ssh", """
        printf '%s %s %s %s %s %s %s %s %s %s\\n' "$1" "$2" "$3" "$4" "$5" "$6" "$7" "$8" "$9" "${10}" >> "\(bin.path("ssh.log"))"
        case " $* " in
          *" BatchMode=yes "*) ;;
          *) read -r password < /dev/tty; exec sleep 600 ;;
        esac
        case " $* " in
          *" RemoteCommand=none "*) ;;
          *) echo 'Cannot execute command-line and remote command.' >&2; exit 255 ;;
        esac
        \(login)
        """)
        try bin.script("uname", """
        case "$1" in
          -s) echo \(uname.kernel) ;;
          -m) echo \(uname.machine) ;;
        esac
        """)
        try bin.script("systemctl", """
        uid="$(id -u)"
        [ "$XDG_RUNTIME_DIR" = "/run/user/$uid" ] && [ "$DBUS_SESSION_BUS_ADDRESS" = "unix:path=/run/user/$uid/bus" ] || { echo 'Failed to connect to bus: No medium found' >&2; exit 1; }
        printf '%s\\n' "$*" >> "\(bin.path("systemctl.log"))"
        """)
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
        #expect(items["gh"]?.command == "ssh -t 'box' gh auth login")
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
        #expect(server.bin.read("ssh.log").split(separator: "\n").allSatisfy { $0 == "-T -o BatchMode=yes -o RemoteCommand=none -o RequestTTY=no -o ConnectTimeout=10 box" })
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

    @Test(.timeLimit(.minutes(1))) func anSshFailureThatIsNotAboutLoginShowsWhatSshSaid() async throws {
        let server = try FakeServer(unreachable: true)
        let setup = server.setup()
        await setup.setUp()
        let error = try #require(setup.error)
        #expect(error.contains("Connection refused"))
        #expect(!error.contains("password"))
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

    @Test func theStepListHidesTheServerStepForThisMac() {
        var flow = FirstRunFlow()
        #expect(flow.steps == [.welcome, .location, .setup, .workspace, .done])
        flow.kind = .ssh(host: "box")
        #expect(flow.steps == FirstRunStep.allCases)
    }

    @Test func theButtonBarFollowsTheStep() {
        var flow = FirstRunFlow()
        #expect(flow.canGoBack == false)
        #expect(flow.primaryTitle == "Get started")
        flow.next()
        #expect(flow.canGoBack)
        #expect(flow.primaryTitle == "Continue")
        flow.step = .done
        #expect(flow.primaryTitle == "Open agentws")
    }

    @Test func aStepIsCompleteOnceTheFlowHasPassedIt() {
        var flow = FirstRunFlow()
        flow.kind = .ssh(host: "box")
        flow.step = .setup
        #expect(flow.isComplete(.host))
        #expect(!flow.isComplete(.setup))
        #expect(!flow.isComplete(.done))
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

    @Test func theProbeKnowsTheRemoteHomeForBrowsing() {
        #expect(Probe.parse("home=/home/me\n").home == "/home/me")
    }

    @Test func browsingAFolderListsItsSubfoldersWithoutRegisteringIt() async throws {
        let caller = SettingsCaller()
        caller.replies["workspace.dirs"] = #"{"dirs":[{"Name":"api","Path":"/u/code/api","Git":1},{"Name":"notes","Path":"/u/code/notes","Git":0}]}"#
        let settings = ServerSettings(caller: caller)
        let dirs = await settings.dirs("/u/code")
        #expect(dirs == ["/u/code/api", "/u/code/notes"])
        #expect(caller.calls.map(\.method) == ["workspace.dirs"])
        #expect(caller.calls.first?.params == #"{"path":"\/u\/code"}"#)
    }

    @Test func settingsHaveAServersTabThatIsNotPerServer() {
        #expect(SettingsTab.allCases.contains(.servers))
        #expect(SettingsTab.servers.title == "Servers")
        #expect(!SettingsTab.servers.perServer)
    }

    @Test func theCopiedGhLoginCommandQuotesTheHost() {
        let probe = Probe.parse("tool=gh\n")
        let item = Checklist.items(server: .ssh(host: "$(touch x)"), probe: probe, appBuild: "v1").first { $0.id == "gh" }
        #expect(item?.command == "ssh -t '$(touch x)' gh auth login")
    }

    @Test(.timeLimit(.minutes(1))) func aSecondSetupWhileOneRunsIsIgnored() async throws {
        let server = try FakeServer()
        let setup = server.setup()
        async let first: Void = setup.setUp()
        async let second: Void = setup.setUp()
        _ = await (first, second)
        #expect(server.bin.read("daemon.log") == "setup daemon\n")
    }
}
