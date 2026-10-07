#if canImport(SwiftUI) && canImport(UserNotifications) && canImport(AppKit)
import AgentwsKit
import AppKit
import Observation
import UserNotifications

public enum BannerAction: Sendable, Equatable {
    case allow(String)
    case reply(String, String)
    case open(String)
}

@MainActor
@Observable
public final class UserNotifier: NSObject, Notifier, UNUserNotificationCenterDelegate {
    public private(set) var problem: String?
    private var authorized = false
    private var asking = false
    private var waiting: [String: BannerPost] = [:]
    private var started = false
    private let handle: @MainActor (BannerAction) -> Void

    public init(handle: @escaping @MainActor (BannerAction) -> Void) {
        self.handle = handle
    }

    private var center: UNUserNotificationCenter? {
        Bundle.main.bundleIdentifier == nil ? nil : UNUserNotificationCenter.current()
    }

    public func start() {
        guard !started else { return }
        started = true
        guard let center else {
            problem = "Banners are off: run agentws from its app bundle to get them."
            return
        }
        center.delegate = self
        let allow = UNNotificationAction(identifier: "allow", title: "Allow")
        let open = UNNotificationAction(identifier: "open", title: "Open", options: [.foreground])
        let reply = UNTextInputNotificationAction(identifier: "reply", title: "Reply…", textInputButtonTitle: "Send", textInputPlaceholder: "Message")
        center.setNotificationCategories([
            UNNotificationCategory(identifier: "permission", actions: [allow, open], intentIdentifiers: []),
            UNNotificationCategory(identifier: "waiting", actions: [reply, open], intentIdentifiers: []),
            UNNotificationCategory(identifier: "other", actions: [open], intentIdentifiers: []),
        ])
        asking = true
        center.requestAuthorization(options: [.alert, .sound]) { [weak self] granted, error in
            let reason = error?.localizedDescription
            Task { @MainActor in self?.authorize(granted, reason) }
        }
    }

    private func authorize(_ granted: Bool, _ reason: String?) {
        asking = false
        authorized = granted
        let queued = waiting.values
        waiting = [:]
        if granted {
            problem = nil
            for banner in queued { post(banner) }
        } else if let reason {
            problem = "Banners are off (" + reason + "). The menu bar and Dock badge still show who needs you."
        } else {
            problem = "Banners are off: allow agentws in System Settings › Notifications."
        }
    }

    public func post(_ banner: BannerPost) {
        guard let center else { return }
        guard authorized else {
            if asking { waiting[banner.id] = banner }
            return
        }
        let content = UNMutableNotificationContent()
        content.title = banner.title
        content.body = banner.body
        content.threadIdentifier = banner.id
        content.sound = banner.sound ? .default : nil
        content.categoryIdentifier = switch banner.kind {
        case .permission: "permission"
        case .waiting: "waiting"
        case .other: "other"
        }
        center.add(UNNotificationRequest(identifier: banner.id, content: content, trigger: nil)) { _ in }
    }

    public func withdraw(_ id: String) {
        waiting[id] = nil
        guard let center else { return }
        center.removeDeliveredNotifications(withIdentifiers: [id])
        center.removePendingNotificationRequests(withIdentifiers: [id])
    }

    public nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter, willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .list, .sound])
    }

    public nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let id = response.notification.request.identifier
        let action: BannerAction? = switch response.actionIdentifier {
        case "allow": .allow(id)
        case "reply": (response as? UNTextInputNotificationResponse).map { .reply(id, $0.userText) }
        case UNNotificationDismissActionIdentifier: nil
        default: .open(id)
        }
        completionHandler()
        guard let action else { return }
        Task { @MainActor in self.handle(action) }
    }
}
#endif
