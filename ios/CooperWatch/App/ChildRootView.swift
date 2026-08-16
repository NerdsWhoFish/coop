import SwiftUI

struct ChildRootView: View {
  @Bindable var model: ChildAppModel

  var body: some View {
    Group {
      switch model.destination {
      case .pairing:
        PairingView(model: model)
      case .watch:
        ChildDashboard(model: model)
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
