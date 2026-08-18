#if canImport(UIKit)
  import UIKit
  import UserNotifications

  /// Routes APNs registration and notification taps between UIKit's app-level
  /// delegates and whoever in the SwiftUI world is listening. Shared by both
  /// apps; each app's delegate forwards its UIKit callbacks here.
  @MainActor
  public enum PushRegistration {
    public static var onToken: ((String) -> Void)?
    public static var onNotification: (() -> Void)?

    /// Asks for permission and registers. Safe to call repeatedly; iOS only
    /// prompts once and re-registration just refreshes the token.
    public static func register() {
      Task {
        let center = UNUserNotificationCenter.current()
        let options: UNAuthorizationOptions = [.alert, .sound, .badge]
        guard (try? await center.requestAuthorization(options: options)) == true else { return }
        UIApplication.shared.registerForRemoteNotifications()
      }
    }

    /// APNs hands tokens over as raw bytes; the registration API wants hex.
    public static func hexToken(from deviceToken: Data) -> String {
      deviceToken.map { String(format: "%02x", $0) }.joined()
    }
  }

  public final class PushNotificationDelegate: NSObject, UNUserNotificationCenterDelegate {
    public func userNotificationCenter(
      _ center: UNUserNotificationCenter,
      willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
      [.banner, .sound]
    }

    public func userNotificationCenter(
      _ center: UNUserNotificationCenter,
      didReceive response: UNNotificationResponse
    ) async {
      await MainActor.run { PushRegistration.onNotification?() }
    }
  }
#endif
