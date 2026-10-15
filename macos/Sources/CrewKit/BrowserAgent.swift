import Foundation

public struct BrowserCall: Decodable, Sendable, Equatable {
    public var call: UInt64
    public var session: String
    public var command: String
    public var args: [String: JSONValue]

    public init(call: UInt64, session: String, command: String, args: [String: JSONValue]) {
        self.call = call
        self.session = session
        self.command = command
        self.args = args
    }

    private enum CodingKeys: String, CodingKey {
        case call, session, command, args
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        call = try c.decode(UInt64.self, forKey: .call)
        session = try c.decode(String.self, forKey: .session)
        command = try c.decode(String.self, forKey: .command)
        args = (try? c.decodeIfPresent([String: JSONValue].self, forKey: .args)) ?? [:]
    }

    private struct Line: Decodable {
        var browser: BrowserCall?
    }

    public static func decode(line: Data) -> BrowserCall? {
        (try? JSONDecoder().decode(Line.self, from: line))?.browser
    }

    func string(_ key: String) -> String? {
        if case let .string(value) = args[key] { return value }
        return nil
    }

    func ref() -> String? {
        guard let value = string("ref"), value.hasPrefix("@e"), value.count > 2, value.dropFirst(2).allSatisfy(\.isNumber) else { return nil }
        return String(value.dropFirst())
    }
}

public enum BrowserRefusal: Error, Equatable, Sendable {
    case unknown(String)
    case missing(String)

    public var message: String {
        switch self {
        case let .unknown(command): "unknown browser command \(command)"
        case let .missing(field): "the call has no valid \(field)"
        }
    }
}

public enum BrowserStep: Equatable, Sendable {
    case goto(String)
    case script(String, [String: String])
    case until(String, [String: String])
    case sleep(Int)
    case screenshot
    case tabs
    case cookies(String?)
    case device(DevicePreset)

    public static func of(_ call: BrowserCall) throws(BrowserRefusal) -> BrowserStep {
        switch call.command {
        case "goto":
            guard let url = call.string("url"), !url.isEmpty else { throw .missing("url") }
            return .goto(url)
        case "snapshot":
            return .script(BrowserScript.snapshot, [:])
        case "click":
            guard let ref = call.ref() else { throw .missing("ref") }
            return .script(BrowserScript.click, ["ref": ref])
        case "fill":
            guard let ref = call.ref() else { throw .missing("ref") }
            guard let text = call.string("text") else { throw .missing("text") }
            return .script(BrowserScript.fill, ["ref": ref, "text": text])
        case "type":
            guard let text = call.string("text") else { throw .missing("text") }
            return .script(BrowserScript.type, ["text": text])
        case "press":
            guard let key = call.string("key"), !key.isEmpty else { throw .missing("key") }
            return .script(BrowserScript.press, ["key": key])
        case "eval":
            guard let js = call.string("js") else { throw .missing("js") }
            return .script(BrowserScript.eval, ["js": js])
        case "wait":
            if case let .number(ms) = call.args["ms"], ms >= 0 { return .sleep(Int(ms)) }
            if let ref = call.ref() { return .until(BrowserScript.hasRef, ["ref": ref]) }
            if let text = call.string("text") { return .until(BrowserScript.hasText, ["text": text]) }
            throw .missing("ref, ms or text")
        case "screenshot":
            return .screenshot
        case "tabs":
            return .tabs
        case "cookies":
            return .cookies(call.string("domain"))
        case "set-device":
            guard let name = call.string("device"), let device = DevicePreset.allCases.first(where: { $0.rawValue.lowercased() == name.lowercased() }) else {
                throw .missing("device")
            }
            return .device(device)
        default:
            throw .unknown(call.command)
        }
    }
}

public enum BrowserScript {
    public static let snapshot = #"""
    const skip = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "SVG", "HEAD"]);
    for (const old of document.querySelectorAll("[data-crew-ref]")) old.removeAttribute("data-crew-ref");
    let next = 1;
    const lines = [];
    const visible = (el) => {
      const box = el.getBoundingClientRect();
      const style = getComputedStyle(el);
      return box.width > 0 && box.height > 0 && style.visibility !== "hidden" && style.display !== "none";
    };
    const roleOf = (el) => {
      const role = el.getAttribute("role");
      if (role) return role;
      const tag = el.tagName.toLowerCase();
      if (tag === "a" && el.hasAttribute("href")) return "link";
      if (tag === "button" || tag === "summary") return "button";
      if (tag === "select") return "combobox";
      if (tag === "textarea") return "textbox";
      if (tag === "img") return "img";
      if (/^h[1-6]$/.test(tag)) return "heading";
      if (tag === "input") {
        const type = (el.getAttribute("type") || "text").toLowerCase();
        if (type === "hidden") return "";
        if (type === "checkbox" || type === "radio") return type;
        if (type === "submit" || type === "button" || type === "reset") return "button";
        return "textbox";
      }
      return "";
    };
    const nameOf = (el) => {
      const label = el.getAttribute("aria-label") || el.getAttribute("alt") || el.getAttribute("title") || el.getAttribute("placeholder");
      if (label) return label;
      if (el.id) {
        const labelled = document.querySelector(`label[for="${CSS.escape(el.id)}"]`);
        if (labelled) return labelled.innerText;
      }
      if (el.tagName === "INPUT" && el.value) return el.value;
      return (el.innerText || "").trim();
    };
    const walk = (el, depth) => {
      if (skip.has(el.tagName.toUpperCase()) || !visible(el)) return;
      const role = roleOf(el);
      let inner = depth;
      if (role) {
        const ref = "e" + next++;
        el.setAttribute("data-crew-ref", ref);
        const name = nameOf(el).replace(/\s+/g, " ").slice(0, 100);
        lines.push("  ".repeat(depth) + "- " + role + (name ? " " + JSON.stringify(name) : "") + " [@" + ref + "]");
        inner = depth + 1;
      } else if (el.children.length === 0) {
        const text = (el.innerText || "").trim().replace(/\s+/g, " ").slice(0, 200);
        if (text) lines.push("  ".repeat(depth) + "- text " + JSON.stringify(text));
      }
      for (const child of el.children) walk(child, inner);
    };
    if (document.body) walk(document.body, 0);
    return {text: "# " + document.title + " — " + location.href + "\n" + lines.join("\n"), url: location.href, title: document.title, refs: next - 1};
    """#

    public static let click = #"""
    const el = document.querySelector(`[data-crew-ref="${ref}"]`);
    if (!el) throw new Error("no element @" + ref + ": take a new snapshot");
    el.scrollIntoView({block: "center"});
    el.click();
    return "clicked @" + ref;
    """#

    public static let fill = #"""
    const el = document.querySelector(`[data-crew-ref="${ref}"]`);
    if (!el) throw new Error("no element @" + ref + ": take a new snapshot");
    el.focus();
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(proto, "value");
    if (setter && setter.set) setter.set.call(el, text); else el.value = text;
    el.dispatchEvent(new Event("input", {bubbles: true}));
    el.dispatchEvent(new Event("change", {bubbles: true}));
    return "filled @" + ref;
    """#

    public static let type = #"""
    const el = document.activeElement;
    if (!el || el === document.body) throw new Error("no focused element: click or fill one first");
    if ("value" in el) {
      el.value = el.value + text;
      el.dispatchEvent(new Event("input", {bubbles: true}));
    } else if (el.isContentEditable) {
      document.execCommand("insertText", false, text);
    }
    return "typed " + text.length + " characters";
    """#

    public static let press = #"""
    const el = document.activeElement || document.body;
    const options = {key: key, bubbles: true, cancelable: true};
    const allowed = el.dispatchEvent(new KeyboardEvent("keydown", options));
    el.dispatchEvent(new KeyboardEvent("keyup", options));
    if (allowed && key === "Enter" && el.form) {
      if (el.form.requestSubmit) el.form.requestSubmit(); else el.form.submit();
    }
    return "pressed " + key;
    """#

    public static let eval = #"""
    const value = await (0, eval)(js);
    return value === undefined ? null : JSON.parse(JSON.stringify(value));
    """#

    public static let hasRef = #"""
    return document.querySelector(`[data-crew-ref="${ref}"]`) !== null;
    """#

    public static let hasText = #"""
    return document.body !== null && document.body.innerText.includes(text);
    """#

    public static let frames = #"""
    return Array.from(document.querySelectorAll("iframe, frame")).map((f) => f.src).filter((s) => s);
    """#
}

public enum BrowserOutcome: Sendable, Equatable {
    case result(JSONValue)
    case error(String)
}

public struct BrowserReply: Encodable, Sendable, Equatable {
    public var call: UInt64
    public var result: JSONValue?
    public var error: String?

    public init(call: UInt64, result: JSONValue) {
        self.call = call
        self.result = result
    }

    public init(call: UInt64, error: String) {
        self.call = call
        self.error = error
    }

    public init(call: UInt64, outcome: BrowserOutcome) {
        switch outcome {
        case let .result(value): self.init(call: call, result: value)
        case let .error(message): self.init(call: call, error: message)
        }
    }

    private enum CodingKeys: String, CodingKey {
        case call, result, error
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(call, forKey: .call)
        if let error {
            try c.encode(error, forKey: .error)
        } else {
            try c.encode(result ?? .null, forKey: .result)
        }
    }

    public func request(id: UInt64, build: String) throws -> Data {
        try Request(id: id, method: "browser.reply", params: self, build: build).encoded()
    }
}

public struct BrowserPolicy: Sendable, Equatable {
    public var serverHost: String

    public init(serverHost: String) {
        self.serverHost = serverHost.lowercased()
    }

    public func needsLookup(url: String) -> String? {
        guard let parts = URLComponents(string: url), let scheme = parts.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let host = parts.host?.lowercased(), !host.isEmpty, host != serverHost else { return nil }
        return host
    }

    public func refusal(url: String, addresses: [String]) -> String? {
        guard let parts = URLComponents(string: url), let scheme = parts.scheme?.lowercased() else { return "not a URL: \(url)" }
        if scheme == "about" { return nil }
        guard scheme == "http" || scheme == "https" else { return "only http and https are allowed" }
        guard let host = parts.host?.lowercased(), !host.isEmpty else { return "not a URL: \(url)" }
        if host == serverHost { return nil }
        guard !addresses.isEmpty else { return "\(host) does not resolve" }
        if addresses.contains(where: { !Self.isPublic($0) }) { return "\(host) is on a private network" }
        return nil
    }

    public static func isPublic(_ address: String) -> Bool {
        if let v4 = ipv4(address) { return publicV4(v4) }
        guard let v6 = ipv6(address) else { return false }
        if v6[0..<10].allSatisfy({ $0 == 0 }), v6[10] == 0xFF, v6[11] == 0xFF {
            return publicV4(Array(v6[12..<16]))
        }
        if v6.allSatisfy({ $0 == 0 }) { return false }
        if v6[0..<15].allSatisfy({ $0 == 0 }) && v6[15] == 1 { return false }
        if v6[0] & 0xFE == 0xFC { return false }
        if v6[0] == 0xFE && v6[1] & 0xC0 == 0x80 { return false }
        if v6[0] == 0xFF { return false }
        return true
    }

    private static func publicV4(_ b: [UInt8]) -> Bool {
        switch (b[0], b[1]) {
        case (0, _), (10, _), (127, _): false
        case (100, 64...127): false
        case (169, 254): false
        case (172, 16...31): false
        case (192, 168): false
        case (224...255, _): false
        default: true
        }
    }

    private static func ipv4(_ text: String) -> [UInt8]? {
        let parts = text.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 4 else { return nil }
        let bytes = parts.compactMap { UInt8($0) }
        return bytes.count == 4 ? bytes : nil
    }

    private static func ipv6(_ text: String) -> [UInt8]? {
        let address = text.split(separator: "%").first.map(String.init) ?? text
        guard address.contains(":") else { return nil }
        var tail: [UInt8] = []
        var body = address
        if let last = address.split(separator: ":").last, last.contains(".") {
            guard let v4 = ipv4(String(last)) else { return nil }
            tail = v4
            body = String(address.dropLast(last.count))
            if body.hasSuffix(":") && !body.hasSuffix("::") { body.removeLast() }
        }
        let halves = body.components(separatedBy: "::")
        guard halves.count <= 2 else { return nil }
        func groups(_ part: String) -> [UInt16]? {
            if part.isEmpty { return [] }
            let items = part.split(separator: ":", omittingEmptySubsequences: false).map { UInt16($0, radix: 16) }
            return items.contains(nil) ? nil : items.compactMap { $0 }
        }
        guard let head = groups(halves[0]) else { return nil }
        let rest = halves.count == 2 ? groups(halves[1]) : []
        guard let rest else { return nil }
        let needed = 8 - tail.count / 2
        var all = head
        if halves.count == 2 {
            guard head.count + rest.count < needed else { return nil }
            all += Array(repeating: 0, count: needed - head.count - rest.count) + rest
        }
        guard all.count == needed else { return nil }
        return all.flatMap { [UInt8($0 >> 8), UInt8($0 & 0xFF)] } + tail
    }
}

public final class BrowserLink: Sendable {
    public enum Outcome: Sendable, Equatable {
        case ended
        case stop
    }

    public typealias Handler = @Sendable (BrowserCall) async -> BrowserOutcome

    private let endpoint: Endpoint
    private let build: String
    private let environment: [String: String]?
    private let handler: Handler

    public init(endpoint: Endpoint, build: String, environment: [String: String]? = nil, handler: @escaping Handler) {
        self.endpoint = endpoint
        self.build = build
        self.environment = environment
        self.handler = handler
    }

    private struct Header: Decodable {
        var id: UInt64
        var error: RPCError?
    }

    public func run(sleep: @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) async {
        var backoff = Backoff()
        while !Task.isCancelled {
            let started = ContinuousClock.now
            if await runOnce() == .stop { return }
            if ContinuousClock.now - started > .seconds(30) { backoff.reset() }
            do { try await sleep(backoff.next()) } catch { return }
        }
    }

    public func runOnce() async -> Outcome {
        let process: LineProcess
        do {
            process = try LineProcess(argv: endpoint.argv, environment: environment)
        } catch {
            return .ended
        }
        defer { process.close() }
        do {
            try process.send(Request(id: 1, method: "browser.attach", params: [String: String](), build: build).encoded())
        } catch {
            return .ended
        }
        let replies = ReplyIDs()
        return await withTaskGroup(of: Void.self) { group in
            defer { group.cancelAll() }
            for await line in process.lines {
                if let call = BrowserCall.decode(line: line) {
                    group.addTask { [handler, build] in
                        let outcome = await handler(call)
                        let id = await replies.next()
                        if let data = try? BrowserReply(call: call.call, outcome: outcome).request(id: id, build: build) {
                            try? process.send(data)
                        }
                    }
                    continue
                }
                guard let header = try? JSONDecoder().decode(Header.self, from: line), let error = header.error else { continue }
                if header.id == 1 || header.id == 0 {
                    return error.kind == .versionMismatch ? .stop : .ended
                }
            }
            await group.waitForAll()
            return .ended
        }
    }
}

actor ReplyIDs {
    private var last: UInt64 = 1

    func next() -> UInt64 {
        last += 1
        return last
    }
}
