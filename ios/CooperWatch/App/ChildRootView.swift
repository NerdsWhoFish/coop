import CoopKit
import SwiftUI

struct ChildRootView: View {
  @Bindable var model: ChildAppModel

  var body: some View {
    Group {
      if let release = model.requiredUpdate {
        RequiredUpdateView(release: release, audience: .child) {
          await model.checkForRequiredUpdate()
        }
      } else {
        switch model.destination {
        case .launching:
          CoopSplashView(hasError: model.errorMessage != nil) {
            Task { await model.launch() }
          }
        case .pairing:
          PairingView(model: model)
        case .watch:
          ChildDashboard(model: model)
        }
      }
    }
    .alert("Something got tangled", isPresented: errorIsPresented) {
      Button("OK", role: .cancel) {}
    } message: {
      Text(model.errorMessage ?? "Unknown error")
    }
  }

  private var errorIsPresented: Binding<Bool> {
    Binding(
      get: { model.errorMessage != nil },
      set: { if !$0 { model.errorMessage = nil } }
    )
  }
}
