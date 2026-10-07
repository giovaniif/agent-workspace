#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

public struct MenuScene {
    public var menu: AttentionMenu
    public var connection: ConnectionStatus
    public var notifications: String?
    public var bridges: [BridgeAgent]
    public var message: String?

    public init(
        menu: AttentionMenu, connection: ConnectionStatus, notifications: String? = nil,
        bridges: [BridgeAgent] = [], message: String? = nil
    ) {
        self.menu = menu
        self.connection = connection
        self.notifications = notifications
        self.bridges = bridges
        self.message = message
    }

    public static func seeded() -> MenuScene {
        MenuScene(
            menu: AttentionMenu(state: Seed.window, now: Seed.now), connection: .live,
            bridges: [BridgeAgent(label: "dev.agentws.bridge.box", host: "box", remoteBinary: nil)]
        )
    }
}

public struct MenuActions {
    public var allow: (String) -> Void = { _ in }
    public var always: (String) -> Void = { _ in }
    public var open: (String) -> Void = { _ in }
    public var newSession: () -> Void = {}
    public var openApp: () -> Void = {}
    public var removeBridges: () -> Void = {}
    public var dismissMessage: () -> Void = {}
    public var quit: () -> Void = {}

    public init() {}
}

public struct AttentionMenuView: View {
    let scene: MenuScene
    let actions: MenuActions
    @Environment(\.colorScheme) private var scheme

    public init(scene: MenuScene, actions: MenuActions = MenuActions()) {
        self.scene = scene
        self.actions = actions
    }

    public var body: some View {
        let theme = Theme(scheme)
        let menu = scene.menu
        VStack(alignment: .leading, spacing: 10) {
            if let banner = ConnectionBanner(scene.connection) {
                note(banner.title + ": " + banner.detail, tone: banner.tone, theme)
            }
            if let message = scene.message {
                HStack {
                    note(message, tone: .peach, theme)
                    Button(action: actions.dismissMessage) { Image(systemName: "xmark") }.buttonStyle(.plain)
                }
            }
            if let off = scene.notifications {
                note(off, tone: .grey, theme)
            }
            if !scene.bridges.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    note("The old notify bridge (" + scene.bridges.map(\.host).joined(separator: ", ") + ") also posts banners, so they come twice.", tone: .peach, theme)
                    Button("Remove old bridge", action: actions.removeBridges).font(.system(size: 12, weight: .medium))
                }
            }
            section("Needs you", menu.needsYou, theme) { row in
                if row.permission {
                    small("Allow", tone: .red, theme) { actions.allow(row.id) }
                    small("Always", tone: .surface, theme) { actions.always(row.id) }
                }
                small("Open", tone: .surface, theme) { actions.open(row.id) }
            }
            section("Done · unread", menu.doneUnread, theme) { row in
                small("Open", tone: .surface, theme) { actions.open(row.id) }
            }
            section("Working", menu.working, theme) { row in
                small("Open", tone: .surface, theme) { actions.open(row.id) }
            }
            if !menu.meters.isEmpty {
                Divider()
                HStack(spacing: 12) {
                    ForEach(menu.meters) { QuotaMeterView(meter: $0) }
                }
            }
            Divider()
            menuButton("New session…", action: actions.newSession)
            menuButton("Open agentws", action: actions.openApp)
            menuButton("Quit agentws", action: actions.quit)
        }
        .padding(12)
        .frame(width: 340, alignment: .leading)
        .foregroundStyle(theme(.text))
        .background(theme(.base))
    }

    private func note(_ text: String, tone: Tone, _ theme: Theme) -> some View {
        Text(text)
            .font(.system(size: 11))
            .foregroundStyle(theme(.subtext))
            .padding(.horizontal, 8)
            .padding(.vertical, 5)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 5).fill(theme(tone).opacity(0.14)))
    }

    @ViewBuilder
    private func section<Buttons: View>(
        _ title: String, _ rows: [AttentionRow], _ theme: Theme, @ViewBuilder buttons: @escaping (AttentionRow) -> Buttons
    ) -> some View {
        if !rows.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text(title.uppercased()).font(.system(size: 10, weight: .semibold)).foregroundStyle(theme(.subtext))
                ForEach(rows) { row in
                    HStack(spacing: 6) {
                        Text(row.style.symbol).foregroundStyle(theme(row.style.tone)).frame(width: 14)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(row.name).font(.system(size: 12, weight: .medium)).lineLimit(1)
                            Text(row.where).font(Metrics.mono).foregroundStyle(theme(.subtext)).lineLimit(1)
                        }
                        Spacer(minLength: 4)
                        buttons(row)
                    }
                }
            }
        }
    }

    private func small(_ title: String, tone: Tone, _ theme: Theme, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .font(.system(size: 11, weight: .medium))
                .padding(.horizontal, 7)
                .padding(.vertical, 3)
                .background(RoundedRectangle(cornerRadius: 4).fill(theme(tone)))
                .foregroundStyle(tone == .red ? theme(.base) : theme(.text))
        }
        .buttonStyle(.plain)
    }

    private func menuButton(_ title: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title).font(.system(size: 12)).frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
#endif
