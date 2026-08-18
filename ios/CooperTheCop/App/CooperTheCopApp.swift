import CoopKit
import SwiftUI
import UIKit
import UserNotifications

@MainActor
final class CooperTheCopAppDelegate: NSObject, UIApplicationDelegate {
  private let notificationDelegate = PushNotificationDelegate()

  func application(
    _: UIApplication,
    didFinishLaunchingWithOptions _: [UIApplication.LaunchOptionsKey: Any]? = nil
  ) -> Bool {
    UNUserNotificationCenter.current().delegate = notificationDelegate
    return true
  }

  func application(
    _: UIApplication,
    didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
  ) {
    PushRegistration.onToken?(PushRegistration.hexToken(from: deviceToken))
  }

  func application(
    _: UIApplication,
    didFailToRegisterForRemoteNotificationsWithError _: any Error
  ) {
    // Push is an optional layer; a simulator or a denied capability is fine.
  }
}

@main
struct CooperTheCopApp: App {
  @Environment(\.scenePhase) private var scenePhase
  @UIApplicationDelegateAdaptor(CooperTheCopAppDelegate.self) private var appDelegate
  @State private var model = AppModel()

  var body: some Scene {
    WindowGroup {
      RootView(model: model)
        .preferredColorScheme(.dark)
        .task {
          if !AppModel.isUIPreview {
            await model.checkForRequiredUpdate()
            if model.requiredUpdate == nil { await model.restore() }
          }
        }
        .onChange(of: scenePhase) { _, phase in
          guard phase == .active, !AppModel.isUIPreview else { return }
          Task { await model.checkForRequiredUpdate() }
        }
    }
  }
}
