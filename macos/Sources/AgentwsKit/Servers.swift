import Foundation
import Observation

public enum ServerKind: Codable, Hashable, Sendable, Identifiable {
    case thisMac
    case ssh(host: String)

    public static let remoteBinary = "~/.local/bin/agentws"

    public var id: String { name }

    public var name: String {
        switch self {
        case .thisMac: "This Mac"
        case let .ssh(host): host
        }
    }

    public var isRemote: Bool { self != .thisMac }

    public func endpoint(localBinary: String) -> Endpoint {
        switch self {
        case .thisMac: .local(binary: localBinary)
        case let .ssh(host): .ssh(host: host, remoteBinary: Self.remoteBinary)
        }
    }
}

public enum SSHConfig {
    public static func hosts(_ text: String) -> [String] {
        var hosts: [String] = []
        for line in text.split(whereSeparator: \.isNewline) {
            let words = line.split(whereSeparator: { $0 == " " || $0 == "\t" || $0 == "=" }).map(String.init)
            guard let keyword = words.first, keyword.lowercased() == "host" else { continue }
            for name in words.dropFirst() where !name.contains(where: { "*?!".contains($0) }) && !hosts.contains(name) {
                hosts.append(name)
            }
        }
        return hosts
    }

    public static func target(_ text: String) -> String? {
        let target = text.trimmingCharacters(in: .whitespaces)
        guard !target.isEmpty, !target.hasPrefix("-"), !target.contains(where: { $0.isWhitespace || $0 == "'" || $0 == "\"" }) else { return nil }
        return target
    }
}

public struct Platform: Equatable, Sendable {
    public var os: String
    public var arch: String

    public init?(kernel: String, machine: String) {
        switch kernel {
        case "Linux": os = "linux"
        case "Darwin": os = "darwin"
        default: return nil
        }
        switch machine {
        case "x86_64", "amd64": arch = "amd64"
        case "aarch64", "arm64": arch = "arm64"
        default: return nil
        }
    }

    public var directory: String { "\(os)_\(arch)" }
}

public struct DaemonCheck: Codable, Equatable, Sendable {
    public var installed: Bool
    public var running: Bool
    public var linger: Bool
}

public struct Probe: Equatable, Sendable {
    public var kernel = ""
    public var machine = ""
    public var user = ""
    public var build: String?
    public var daemon: DaemonCheck?
    public var tools: Set<String> = []
    public var ghSignedIn = false
    public var latency: Duration?

    public var platform: Platform? { Platform(kernel: kernel, machine: machine) }

    public static func parse(_ output: String) -> Probe {
        var probe = Probe()
        for line in output.split(whereSeparator: \.isNewline) {
            guard let equals = line.firstIndex(of: "=") else { continue }
            let value = String(line[line.index(after: equals)...]).trimmingCharacters(in: .whitespaces)
            switch line[..<equals] {
            case "kernel": probe.kernel = value
            case "machine": probe.machine = value
            case "user": probe.user = value
            case "build": probe.build = value.isEmpty ? nil : value
            case "daemon": probe.daemon = try? JSONDecoder().decode(DaemonCheck.self, from: Data(value.utf8))
            case "tool": probe.tools.insert(value)
            case "gh": probe.ghSignedIn = value == "signed-in"
            default: break
            }
        }
        return probe
    }
}

public enum CheckState: Equatable, Sendable {
    case ok
    case warning
    case failed
}

public struct CheckItem: Equatable, Sendable, Identifiable {
    public var id: String
    public var title: String
    public var state: CheckState
    public var detail: String
    public var fix: String?
    public var command: String?

    public init(id: String, title: String, state: CheckState, detail: String, fix: String? = nil, command: String? = nil) {
        self.id = id
        self.title = title
        self.state = state
        self.detail = detail
        self.fix = fix
        self.command = command
    }
}

public enum Checklist {
    public static func items(server: ServerKind, probe: Probe, appBuild: String) -> [CheckItem] {
        var items: [CheckItem] = []
        if case let .ssh(host) = server {
            let latency = probe.latency.map { " · \(Int($0 / .milliseconds(1))) ms" } ?? ""
            items.append(CheckItem(id: "ssh", title: "SSH", state: .ok, detail: "\(host) reachable\(latency) · \(probe.kernel) \(probe.machine)"))
        }
        switch probe.build {
        case nil:
            items.append(CheckItem(id: "agentws", title: "agentws", state: .failed, detail: "not installed", fix: "Set up"))
        case let build? where build != appBuild:
            items.append(CheckItem(id: "agentws", title: "agentws", state: .warning, detail: "build \(build), the app is \(appBuild)", fix: "Update server"))
        case let build?:
            items.append(CheckItem(id: "agentws", title: "agentws", state: .ok, detail: build))
        }
        items.append(daemon(probe))
        for tool in ["git", "tmux"] {
            items.append(probe.tools.contains(tool)
                ? CheckItem(id: tool, title: tool, state: .ok, detail: "found")
                : CheckItem(id: tool, title: tool, state: .failed, detail: "not found on PATH; install it with the system's package manager"))
        }
        let login = server.isRemote ? "ssh -t \(server.name) gh auth login" : "gh auth login"
        if !probe.tools.contains("gh") {
            items.append(CheckItem(id: "gh", title: "gh", state: .warning, detail: "not installed; pull request status needs it"))
        } else if !probe.ghSignedIn {
            items.append(CheckItem(id: "gh", title: "gh", state: .warning, detail: "not signed in", fix: "Run gh auth login", command: login))
        } else {
            items.append(CheckItem(id: "gh", title: "gh", state: .ok, detail: "signed in"))
        }
        return items
    }

    static func lingerCommand(_ probe: Probe) -> String? {
        guard probe.kernel == "Linux", let daemon = probe.daemon, daemon.installed, !daemon.linger else { return nil }
        return "sudo loginctl enable-linger \(probe.user)"
    }

    private static func daemon(_ probe: Probe) -> CheckItem {
        guard let daemon = probe.daemon, daemon.installed else {
            return CheckItem(id: "daemon", title: "Daemon", state: .failed, detail: "not set up as a service", fix: "Set up")
        }
        guard daemon.running else {
            return CheckItem(id: "daemon", title: "Daemon", state: .failed, detail: "installed but not running", fix: "Set up")
        }
        if let command = lingerCommand(probe) {
            return CheckItem(id: "daemon", title: "Daemon", state: .warning, detail: "running, but linger is off: the daemon and every session stop at logout", command: command)
        }
        return CheckItem(id: "daemon", title: "Daemon", state: .ok, detail: "running")
    }
}

public struct RunResult: Sendable {
    public var status: Int32
    public var output: String
    public var error: String
}

public protocol ServerShell: Sendable {
    func run(_ script: String, input: Data?) async throws -> RunResult
}

public struct ProcessShell: ServerShell {
    public var server: ServerKind
    public var environment: [String: String]?

    public init(server: ServerKind, environment: [String: String]? = nil) {
        self.server = server
        self.environment = environment
    }

    public func argv(_ script: String) -> [String] {
        switch server {
        case .thisMac:
            ["sh", "-c", script]
        case let .ssh(host):
            ["ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, "sh -c " + shellQuote(script)]
        }
    }

    public func run(_ script: String, input: Data?) async throws -> RunResult {
        let argv = argv(script)
        let environment = environment
        return try await Task.detached {
            let process = Process()
            let stdin = Pipe()
            let stdout = Pipe()
            let stderr = Pipe()
            process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
            process.arguments = argv
            if let environment { process.environment = environment }
            process.standardInput = stdin
            process.standardOutput = stdout
            process.standardError = stderr
            signal(SIGPIPE, SIG_IGN)
            try process.run()
            let writer = stdin.fileHandleForWriting
            Thread.detachNewThread {
                if let input { try? writer.write(contentsOf: input) }
                try? writer.close()
            }
            let errors = Mutexed()
            let reader = stderr.fileHandleForReading
            let done = DispatchSemaphore(value: 0)
            Thread.detachNewThread {
                errors.set(reader.readDataToEndOfFile())
                done.signal()
            }
            let output = stdout.fileHandleForReading.readDataToEndOfFile()
            done.wait()
            process.waitUntilExit()
            return RunResult(
                status: process.terminationStatus,
                output: String(decoding: output, as: UTF8.self),
                error: String(decoding: errors.get(), as: UTF8.self)
            )
        }.value
    }
}

final class Mutexed: @unchecked Sendable {
    private let lock = NSLock()
    private var value = Data()

    func set(_ value: Data) {
        lock.lock()
        self.value = value
        lock.unlock()
    }

    func get() -> Data {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

func shellQuote(_ text: String) -> String {
    "'" + text.replacingOccurrences(of: "'", with: #"'\''"#) + "'"
}

public struct ServerSetupError: Error, Equatable, Sendable {
    public var message: String
}

@MainActor
@Observable
public final class ServerSetup {
    public let server: ServerKind
    public let appBuild: String
    public private(set) var probe: Probe?
    public private(set) var checklist: [CheckItem] = []
    public private(set) var lingerCommand: String?
    public private(set) var error: String?
    public private(set) var notice: String?
    public private(set) var busy = false

    @ObservationIgnored private let bundled: @Sendable (Platform) -> String?
    @ObservationIgnored private let shell: any ServerShell

    public init(server: ServerKind, appBuild: String, bundled: @escaping @Sendable (Platform) -> String?, shell: any ServerShell) {
        self.server = server
        self.appBuild = appBuild
        self.bundled = bundled
        self.shell = shell
    }

    public func check() async {
        await run { _ = try await self.refresh() }
    }

    public func setUp() async {
        await run {
            let before = try await self.refresh()
            guard let platform = before.platform else {
                throw ServerSetupError(message: "agentws has no build for \(before.kernel) \(before.machine); it runs on Linux and macOS, amd64 or arm64")
            }
            var replaced = false
            if self.server.isRemote, before.build != self.appBuild {
                try await self.install(platform)
                replaced = true
            }
            let binary = try self.binary(platform)
            let daemon = try await self.shell.run("\(binary) setup daemon", input: nil)
            guard daemon.status == 0 else { throw self.failure("agentws setup daemon", daemon) }
            if replaced, before.daemon?.running == true {
                let restart = try await self.shell.run(Self.restart(platform), input: nil)
                guard restart.status == 0 else { throw self.failure("restarting the daemon", restart) }
            }
            let after = try await self.refresh()
            guard after.build == self.appBuild else {
                throw ServerSetupError(message: "\(self.server.name) reports build \(after.build ?? "none") after setup, not \(self.appBuild)")
            }
            self.notice = replaced ? "Installed \(self.appBuild) on \(self.server.name)" : "\(self.server.name) is set up"
        }
    }

    private static func restart(_ platform: Platform) -> String {
        platform.os == "darwin"
            ? #"launchctl kickstart -k "gui/$(id -u)/dev.agentws.daemon""#
            : "systemctl --user restart agentws-daemon.service"
    }

    private func binary(_ platform: Platform) throws -> String {
        if server.isRemote { return #""$HOME/.local/bin/agentws""# }
        guard let path = bundled(platform) else { throw ServerSetupError(message: "this app has no bundled agentws for \(platform.directory)") }
        return shellQuote(path)
    }

    private func install(_ platform: Platform) async throws {
        guard let path = bundled(platform), let data = FileManager.default.contents(atPath: path) else {
            throw ServerSetupError(message: "this app has no bundled agentws for \(platform.directory)")
        }
        let script = #"set -e; d="$HOME/.local/bin"; mkdir -p "$d"; t="$d/.agentws.$$"; cat > "$t"; chmod 755 "$t"; mv -f "$t" "$d/agentws""#
        let result = try await shell.run(script, input: data)
        guard result.status == 0 else { throw failure("copying agentws to ~/.local/bin", result) }
    }

    private func refresh() async throws -> Probe {
        let binary: String
        if server.isRemote {
            binary = #""$HOME/.local/bin/agentws""#
        } else {
            binary = shellQuote(bundled(Platform(kernel: "Darwin", machine: Self.machine)!) ?? "agentws")
        }
        let script = """
        PATH="$PATH:$HOME/.local/bin:/usr/local/bin:/opt/homebrew/bin"
        echo "kernel=$(uname -s)"; echo "machine=$(uname -m)"; echo "user=$(id -un)"
        b=\(binary)
        if [ -x "$b" ]; then echo "build=$("$b" version --build 2>/dev/null)"; echo "daemon=$("$b" setup daemon --check 2>/dev/null)"; fi
        for t in git tmux gh; do command -v "$t" >/dev/null 2>&1 && echo "tool=$t"; done
        command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1 && echo gh=signed-in
        true
        """
        let clock = ContinuousClock()
        let start = clock.now
        let result = try await shell.run(script, input: nil)
        guard result.status == 0 else { throw failure("checking the server", result) }
        var probe = Probe.parse(result.output)
        probe.latency = clock.now - start
        self.probe = probe
        checklist = Checklist.items(server: server, probe: probe, appBuild: appBuild)
        lingerCommand = Checklist.lingerCommand(probe)
        return probe
    }

    private static var machine: String {
        #if arch(arm64)
        "arm64"
        #else
        "x86_64"
        #endif
    }

    private func failure(_ what: String, _ result: RunResult) -> ServerSetupError {
        let said = result.error.trimmingCharacters(in: .whitespacesAndNewlines)
        if server.isRemote, result.status == 255 {
            return ServerSetupError(message: "Could not log in to \(server.name) without a password. agentws runs ssh -o BatchMode=yes, so set up key login (ssh-copy-id \(server.name)), load the key into ssh-agent, or use Tailscale SSH. ssh said: \(said)")
        }
        return ServerSetupError(message: "\(what) failed (exit \(result.status))\(said.isEmpty ? "" : ": \(said)")")
    }

    private func run(_ body: @MainActor () async throws -> Void) async {
        busy = true
        defer { busy = false }
        error = nil
        notice = nil
        do {
            try await body()
        } catch let failure as ServerSetupError {
            error = failure.message
        } catch {
            self.error = String(describing: error)
        }
    }
}

public enum FirstRunStep: Int, CaseIterable, Sendable {
    case welcome
    case location
    case host
    case setup
    case workspace
    case done

    public var title: String {
        switch self {
        case .welcome: "Welcome"
        case .location: "Where agents run"
        case .host: "Server"
        case .setup: "Set up"
        case .workspace: "Workspace"
        case .done: "Done"
        }
    }
}

public struct FirstRunFlow: Equatable, Sendable {
    public var step: FirstRunStep = .welcome
    public var kind: ServerKind = .thisMac

    public init() {}

    public mutating func next() {
        switch step {
        case .welcome: step = .location
        case .location: step = kind.isRemote ? .host : .setup
        case .host: step = .setup
        case .setup: step = .workspace
        case .workspace, .done: step = .done
        }
    }

    public mutating func back() {
        switch step {
        case .welcome, .location: step = .welcome
        case .host: step = .location
        case .setup: step = kind.isRemote ? .host : .location
        case .workspace: step = .setup
        case .done: step = .workspace
        }
    }
}

@MainActor
@Observable
public final class ServerList {
    public static let serversKey = "servers"
    public static let selectedKey = "servers.selected"

    public private(set) var servers: [ServerKind]
    public var selected: ServerKind {
        didSet { save() }
    }

    @ObservationIgnored private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        let decoder = JSONDecoder()
        servers = defaults.data(forKey: Self.serversKey).flatMap { try? decoder.decode([ServerKind].self, from: $0) } ?? []
        selected = defaults.data(forKey: Self.selectedKey).flatMap { try? decoder.decode(ServerKind.self, from: $0) } ?? .thisMac
    }

    public func add(_ server: ServerKind) {
        guard !servers.contains(server) else { return }
        servers.append(server)
        servers = servers.filter { !$0.isRemote } + servers.filter(\.isRemote)
        save()
    }

    public func remove(_ server: ServerKind) {
        servers.removeAll { $0 == server }
        if selected == server { selected = servers.first ?? .thisMac }
        save()
    }

    private func save() {
        let encoder = JSONEncoder()
        if let data = try? encoder.encode(servers) { defaults.set(data, forKey: Self.serversKey) }
        if let data = try? encoder.encode(selected) { defaults.set(data, forKey: Self.selectedKey) }
    }
}
