import SwiftUI

@main
struct CooperTheCopApp: App {
  @State private var model = AppModel()

  var body: some Scene {
    WindowGroup {
      RootView(model: model)
        .preferredColorScheme(.dark)
        .task {
          if !AppModel.showsRecommendationPreview { await model.restore() }
        }
    }
  }
}
