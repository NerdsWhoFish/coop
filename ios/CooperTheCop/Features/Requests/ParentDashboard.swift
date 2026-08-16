import SwiftUI

struct ParentDashboard: View {
  @Bindable var model: AppModel

  var body: some View {
    TabView {
      RequestQueueView(model: model)
        .tabItem { Label("Requests", systemImage: "checklist") }

      ChildrenView(model: model)
        .tabItem { Label("Children", systemImage: "person.2.fill") }

      RulesView(model: model)
        .tabItem { Label("Rules", systemImage: "slider.horizontal.3") }

      PlaceholderSection(
        title: "Family",
        detail: "Parents, invitations, server settings, and sign out land here.",
        symbol: "house.fill"
      )
      .safeAreaInset(edge: .bottom) {
        Button("Sign out", role: .destructive) { model.logOut() }
          .padding()
      }
      .tabItem { Label("Family", systemImage: "house.fill") }
    }
    .tint(CoopTheme.cyan)
  }
}

private struct PlaceholderSection: View {
  let title: String
  let detail: String
  let symbol: String

  var body: some View {
    ContentUnavailableView(title, systemImage: symbol, description: Text(detail))
      .coopBackground()
  }
}
