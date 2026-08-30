import CoopKit
import SwiftUI

struct RootView: View {
  @Bindable var model: AppModel

  var body: some View {
    Group {
      if let release = model.requiredUpdate {
        RequiredUpdateView(release: release, audience: .parent) {
          await model.checkForRequiredUpdate()
        }
      } else if AppModel.showsRecommendationPreview {
        NavigationStack {
          RecommendationTuningView(child: AppModel.recommendationPreviewChild, model: model)
        }
      } else if AppModel.showsFamilyRecommendationPreview {
        NavigationStack {
          FamilyRecommendationTuningView(model: model)
        }
      } else if AppModel.showsChildSettingsPreview {
        NavigationStack {
          ChildSettingsView(child: AppModel.recommendationPreviewChild, model: model)
        }
      } else if AppModel.showsFamilyPreview {
        FamilyView(model: model)
      } else {
        switch model.destination {
        case .connecting:
          ConnectView(model: model)
        case .authentication(let needsSetup):
          AuthenticationView(model: model, needsSetup: needsSetup)
        case .totp(let challenge):
          TOTPVerificationView(model: model, challenge: challenge)
        case .dashboard:
          ParentDashboard(model: model)
        }
      }
    }
    .alert("Couldn’t finish that", isPresented: errorIsPresented) {
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
