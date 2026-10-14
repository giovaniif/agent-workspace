import Foundation

public struct PaneSize: Equatable, Sendable {
    public var pane: String
    public var cols: Int
    public var rows: Int

    public init(pane: String, cols: Int, rows: Int) {
        self.pane = pane
        self.cols = cols
        self.rows = rows
    }
}

public enum ControlEvent: Equatable, Sendable {
    case output(pane: String, bytes: [UInt8])
    case begin(number: Int, mine: Bool)
    case reply(number: Int, ok: Bool, lines: [String])
    case layout(window: String, panes: [PaneSize])
    case windowAdd(window: String)
    case windowClose(window: String)
    case pause(pane: String)
    case resume(pane: String)
    case exit(reason: String)
}

public struct ControlParser: Sendable {
    private var block: Int?
    private var body: [String] = []

    public init() {}

    public mutating func feed(_ line: [UInt8]) -> ControlEvent? {
        if let number = block {
            if let end = Self.blockEnd(line), end.number == number {
                block = nil
                let lines = body
                body = []
                return .reply(number: number, ok: end.ok, lines: lines)
            }
            body.append(String(decoding: line, as: UTF8.self))
            return nil
        }
        guard line.first == UInt8(ascii: "%") else { return nil }
        let space = line.firstIndex(of: 0x20) ?? line.endIndex
        let name = String(decoding: line[1..<space], as: UTF8.self)
        let rest = space < line.endIndex ? line[(space + 1)...] : []
        switch name {
        case "output":
            guard let split = rest.firstIndex(of: 0x20) else { return nil }
            return .output(pane: String(decoding: rest[..<split], as: UTF8.self), bytes: Self.unescape(rest[(split + 1)...]))
        case "extended-output":
            guard let split = rest.firstIndex(of: 0x20), let colon = Self.find(Array(" : ".utf8), in: rest) else { return nil }
            return .output(pane: String(decoding: rest[..<split], as: UTF8.self), bytes: Self.unescape(rest[(colon + 3)...]))
        case "begin":
            let fields = Self.words(rest)
            guard fields.count >= 3, let number = Int(fields[1]) else { return nil }
            block = number
            body = []
            return .begin(number: number, mine: (Int(fields[2]) ?? 0) & 1 == 1)
        case "layout-change":
            let fields = Self.words(rest)
            guard fields.count >= 2 else { return nil }
            return .layout(window: fields[0], panes: Layout.leaves(fields[1]))
        case "window-add", "unlinked-window-add":
            return Self.words(rest).first.map { .windowAdd(window: $0) }
        case "window-close", "unlinked-window-close":
            return Self.words(rest).first.map { .windowClose(window: $0) }
        case "pause":
            return Self.words(rest).first.map { .pause(pane: $0) }
        case "continue":
            return Self.words(rest).first.map { .resume(pane: $0) }
        case "exit":
            return .exit(reason: String(decoding: rest, as: UTF8.self))
        default:
            return nil
        }
    }

    private static func blockEnd(_ line: [UInt8]) -> (number: Int, ok: Bool)? {
        let ok: Bool
        if line.starts(with: Array("%end ".utf8)) {
            ok = true
        } else if line.starts(with: Array("%error ".utf8)) {
            ok = false
        } else {
            return nil
        }
        let fields = words(line[...])
        guard fields.count >= 3, let number = Int(fields[2]) else { return nil }
        return (number, ok)
    }

    private static func words(_ bytes: ArraySlice<UInt8>) -> [String] {
        bytes.split(separator: 0x20).map { String(decoding: $0, as: UTF8.self) }
    }

    private static func find(_ needle: [UInt8], in hay: ArraySlice<UInt8>) -> Int? {
        guard hay.count >= needle.count else { return nil }
        var i = hay.startIndex
        while i + needle.count <= hay.endIndex {
            if hay[i..<(i + needle.count)].elementsEqual(needle) { return i }
            i += 1
        }
        return nil
    }

    public static func unescape(_ bytes: ArraySlice<UInt8>) -> [UInt8] {
        var out = [UInt8]()
        out.reserveCapacity(bytes.count)
        bytes.withUnsafeBufferPointer { buffer in
            let n = buffer.count
            var i = 0
            while i < n {
                let b = buffer[i]
                if b == 0x5C, i + 3 < n, isOctal(buffer[i + 1]), isOctal(buffer[i + 2]), isOctal(buffer[i + 3]) {
                    out.append((buffer[i + 1] - 0x30) << 6 | (buffer[i + 2] - 0x30) << 3 | (buffer[i + 3] - 0x30))
                    i += 4
                } else if b == 0x5C, i + 1 < n, buffer[i + 1] == 0x5C {
                    out.append(0x5C)
                    i += 2
                } else {
                    out.append(b)
                    i += 1
                }
            }
        }
        return out
    }

    private static func isOctal(_ b: UInt8) -> Bool { b >= 0x30 && b <= 0x37 }
}

enum Layout {
    static func leaves(_ text: String) -> [PaneSize] {
        let bytes = Array(text.utf8)
        guard let comma = bytes.firstIndex(of: 0x2C) else { return [] }
        var scanner = Scanner(bytes)
        scanner.i = comma + 1
        var out: [PaneSize] = []
        scanner.cell(into: &out)
        return out
    }

    private struct Scanner {
        let bytes: [UInt8]
        var i = 0

        init(_ bytes: [UInt8]) { self.bytes = bytes }

        mutating func eat(_ b: UInt8) -> Bool {
            guard i < bytes.count, bytes[i] == b else { return false }
            i += 1
            return true
        }

        mutating func number() -> Int? {
            var value = 0
            var any = false
            while i < bytes.count, bytes[i] >= 0x30, bytes[i] <= 0x39 {
                value = value * 10 + Int(bytes[i] - 0x30)
                any = true
                i += 1
            }
            return any ? value : nil
        }

        mutating func cell(into out: inout [PaneSize]) {
            guard let cols = number(), eat(0x78), let rows = number(), eat(0x2C), number() != nil, eat(0x2C), number() != nil else { return }
            if eat(0x2C), let id = number() {
                out.append(PaneSize(pane: "%\(id)", cols: cols, rows: rows))
                return
            }
            guard i < bytes.count else { return }
            let close: UInt8 = bytes[i] == 0x7B ? 0x7D : 0x5D
            i += 1
            repeat {
                cell(into: &out)
            } while eat(0x2C)
            _ = eat(close)
        }
    }
}

public struct MatchedReply<Call> {
    public var call: Call
    public var number: Int
    public var ok: Bool
    public var lines: [String]
}

public struct ReplyMatcher<Call> {
    private var queue: [Call] = []
    private var bound: [Int: Call] = [:]

    public init() {}

    public mutating func sent(_ call: Call) {
        queue.append(call)
    }

    public mutating func receive(_ event: ControlEvent) -> MatchedReply<Call>? {
        switch event {
        case let .begin(number, mine):
            if mine, !queue.isEmpty { bound[number] = queue.removeFirst() }
            return nil
        case let .reply(number, ok, lines):
            guard let call = bound.removeValue(forKey: number) else { return nil }
            return MatchedReply(call: call, number: number, ok: ok, lines: lines)
        default:
            return nil
        }
    }

    public mutating func drain() -> [Call] {
        let all = Array(bound.values) + queue
        bound = [:]
        queue = []
        return all
    }
}

extension ReplyMatcher: Sendable where Call: Sendable {}
extension MatchedReply: Sendable where Call: Sendable {}

public enum ControlCommand {
    public static let queryFormat = "#{pane_id} #{window_id} #{pane_width} #{pane_height} #{cursor_x} #{cursor_y} #{mouse_any_flag} #{mouse_button_flag} #{mouse_standard_flag} #{mouse_sgr_flag} #{alternate_on} #{cursor_flag} #{keypad_cursor_flag}"

    private static let hex: [String] = (0...255).map { String(format: "%02x", $0) }

    public static func sendKeys(pane: String, bytes: [UInt8]) -> [String] {
        stride(from: 0, to: bytes.count, by: 256).map { start in
            let chunk = bytes[start..<min(start + 256, bytes.count)]
            return "send-keys -H -t \(pane) " + chunk.map { hex[Int($0)] }.joined(separator: " ")
        }
    }

    public static func size(window: String, cols: Int, rows: Int) -> String {
        "refresh-client -C \(window):\(cols)x\(rows)"
    }

    public static func pauseAfter(seconds: Int) -> String {
        "refresh-client -f pause-after=\(seconds)"
    }

    public static func resume(pane: String) -> String {
        "refresh-client -A '\(pane):continue'"
    }

    public static func capture(pane: String) -> String {
        "capture-pane -p -e -J -S -2000 -t \(pane)"
    }

    public static func query(pane: String) -> String {
        "display-message -p -t \(pane) '\(queryFormat)'"
    }
}

extension Endpoint {
    public func terminalArgv(_ argv: [String]) -> [String] {
        switch self {
        case .local:
            argv
        case let .ssh(host, _):
            sshArgv(host: host, options: ["ServerAliveInterval=15"]) + ["--"]
                + [argv.map { "'" + $0.replacingOccurrences(of: "'", with: #"'\''"#) + "'" }.joined(separator: " ")]
        }
    }
}
