import CoopKit
import SwiftUI
import UIKit
import UserNotifications

@MainActor
final class CooperWatchOrientationDelegate: NSObject, UIApplicationDelegate {
  private static var supportedOrientations: UIInterfaceOrientationMask = .portrait
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

  func application(
    _: UIApplication,
    supportedInterfaceOrientationsFor _: UIWindow?
  ) -> UIInterfaceOrientationMask {
    Self.supportedOrientations
  }

  static func setRegularVideoPlaybackActive(_ isActive: Bool) {
    supportedOrientations =
      isActive
      ? [.portrait, .landscapeLeft, .landscapeRight]
      : .portrait
    let requestedOrientations =
      isActive && ProcessInfo.processInfo.environment["COOP_UI_LANDSCAPE"] == "1"
      ? UIInterfaceOrientationMask.landscapeLeft
      : supportedOrientations

    for case let scene as UIWindowScene in UIApplication.shared.connectedScenes {
      scene.keyWindow?.rootViewController?.setNeedsUpdateOfSupportedInterfaceOrientations()
      scene.requestGeometryUpdate(.iOS(interfaceOrientations: requestedOrientations))
    }
  }
}

@main
struct CooperWatchApp: App {
  @Environment(\.scenePhase) private var scenePhase
  @UIApplicationDelegateAdaptor(CooperWatchOrientationDelegate.self) private var orientationDelegate
  @State private var model = ChildAppModel()

  var body: some Scene {
    WindowGroup {
      ChildRootView(model: model)
        .preferredColorScheme(.dark)
        .task {
          await model.launch()
        }
        .onChange(of: scenePhase) { _, phase in
          if phase == .active {
            Task { await model.sceneBecameActive() }
          } else {
            model.sceneWentInactive()
          }
        }
    }
  }
}
