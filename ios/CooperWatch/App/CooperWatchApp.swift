import SwiftUI
import UIKit

@MainActor
final class CooperWatchOrientationDelegate: NSObject, UIApplicationDelegate {
  private static var supportedOrientations: UIInterfaceOrientationMask = .portrait

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

    for case let scene as UIWindowScene in UIApplication.shared.connectedScenes {
      scene.keyWindow?.rootViewController?.setNeedsUpdateOfSupportedInterfaceOrientations()
      scene.requestGeometryUpdate(.iOS(interfaceOrientations: supportedOrientations))
    }
  }
}

@main
struct CooperWatchApp: App {
  @UIApplicationDelegateAdaptor(CooperWatchOrientationDelegate.self) private var orientationDelegate
  @State private var model = ChildAppModel()

  var body: some Scene {
    WindowGroup {
      ChildRootView(model: model)
        .preferredColorScheme(.dark)
        .task { await model.restore() }
    }
  }
}
