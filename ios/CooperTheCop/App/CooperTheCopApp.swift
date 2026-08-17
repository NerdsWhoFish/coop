import SwiftUI

@main
struct CooperTheCopApp: App {
  @Environment(\.scenePhase) private var scenePhase
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
