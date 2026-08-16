import SwiftUI

@main
struct CooperWatchApp: App {
  @State private var model = ChildAppModel()

  var body: some Scene {
    WindowGroup {
      ChildRootView(model: model)
        .preferredColorScheme(.dark)
        .task { await model.restore() }
    }
  }
}
