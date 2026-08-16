import SwiftUI

@main
struct CooperWatchApp: App {
  @State private var model = ChildAppModel()

  var body: some Scene {
    WindowGroup {
      ChildRootView(model: model)
        .task { await model.restore() }
    }
  }
}
