import Foundation

enum Timestamp {
    static func parse(_ text: String) -> Date? {
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fractional.date(from: text) ?? ISO8601DateFormatter().date(from: text)
    }
}

public struct Header: Sendable, Equatable {
    public var title: String
    public var `where`: String
    public var style: StateStyle
    public var chip: String
    public var prs: [String]
    public var model: String
    public var context: String?

    public init(session: Session, now: Date) {
        title = session.name
        self.where = session.where
        style = StateStyle(state: session.state)
        if let since = session.since.flatMap(Timestamp.parse) {
            chip = style.label + " · " + Self.elapsed(Int(now.timeIntervalSince(since)))
        } else {
            chip = style.label
        }
        prs = session.board.map { pr in
            let mark = Sidebar.checkMark(pr.checks)
            return mark.isEmpty ? "#\(pr.number)" : "#\(pr.number) \(mark)"
        }
        model = [session.model, session.effort].filter { !$0.isEmpty }.joined(separator: " · ")
        context = session.usage.hasContext ? "\(session.usage.contextLeftPercent)% context left" : nil
    }

    public static func elapsed(_ seconds: Int) -> String {
        let s = max(seconds, 0)
        if s < 60 { return "\(s)s" }
        if s < 3_600 { return "\(s / 60)m" }
        return "\(s / 3_600)h \(s % 3_600 / 60)m"
    }
}

public struct QuotaMeter: Sendable, Equatable, Identifiable {
    public var id: String { title }
    public var title: String
    public var used: Int
    public var low: Bool
    public var stale: Bool
    public var caption: String

    public var tone: Tone { low ? .peach : .blue }

    public static func all(_ limits: [JSONValue], now: Date, timeZone: TimeZone = .current) -> [QuotaMeter] {
        let clock = DateFormatter()
        clock.locale = Locale(identifier: "en_US_POSIX")
        clock.timeZone = timeZone
        clock.dateFormat = "HH:mm"
        return limits.compactMap { value in
            guard case let .object(q) = value else { return nil }
            let harness = q["Harness"]?.string ?? ""
            let label = q["label"]?.string ?? ""
            let left = Int(q["LeftPercent"]?.number ?? 0)
            let used = max(0, min(100, 100 - left))
            let resets = q["ResetsAt"]?.number ?? 0
            let staleAt = q["stale_at"]?.string.flatMap(Timestamp.parse)
            var caption = "\(used)%"
            if resets > 0 { caption += " · resets " + clock.string(from: Date(timeIntervalSince1970: resets)) }
            return QuotaMeter(
                title: [harness.prefix(1).uppercased() + harness.dropFirst(), label].filter { !$0.isEmpty }.joined(separator: " "),
                used: used,
                low: q["low"]?.bool ?? false,
                stale: staleAt.map { now > $0 } ?? false,
                caption: caption
            )
        }
    }
}

extension JSONValue {
    var string: String? {
        if case let .string(s) = self { return s }
        return nil
    }

    var number: Double? {
        if case let .number(n) = self { return n }
        return nil
    }

    var bool: Bool? {
        if case let .bool(b) = self { return b }
        return nil
    }
}

public struct ViewTab: Sendable, Equatable, Identifiable {
    public var id: String { title }
    public var title: String
    public var enabled: Bool
}

public enum Toolbar {
    public static let views = [
        ViewTab(title: "Terminal", enabled: true),
        ViewTab(title: "Review", enabled: true),
        ViewTab(title: "Shell", enabled: true),
        ViewTab(title: "nvim", enabled: true),
    ]
    public static let newEnabled = true
}

public struct ConnectionBanner: Sendable, Equatable {
    public var title: String
    public var detail: String
    public var retrying: Bool
    public var action: String?
    public var tone: Tone

    public init?(_ status: ConnectionStatus) {
        switch status {
        case .idle, .live:
            return nil
        case .connecting:
            self.init("Connecting to agentws", "", true, nil, .blue)
        case let .reconnecting(delay):
            self.init("Reconnecting in \(delay.components.seconds) s", "The connection to the daemon dropped.", true, nil, .peach)
        case let .unavailable(message):
            self.init("No agentws daemon · retrying", message, true, nil, .peach)
        case let .versionMismatch(message):
            self.init("Build mismatch · stopped", message, false, "Reconnect", .red)
        }
    }

    private init(_ title: String, _ detail: String, _ retrying: Bool, _ action: String?, _ tone: Tone) {
        self.title = title
        self.detail = detail
        self.retrying = retrying
        self.action = action
        self.tone = tone
    }
}
