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

      FamilyView(model: model)
        .tabItem { Label("Family", systemImage: "house.fill") }
    }
    .tint(CoopTheme.cyan)
    .task { await model.monitorPlayback() }
  }
}
