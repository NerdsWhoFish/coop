import SwiftUI

struct ChildDashboard: View {
  @Bindable var model: ChildAppModel

  var body: some View {
    TabView {
      HomeView(model: model)
        .tabItem { Label("Home", systemImage: "house.fill") }

      SubscriptionsView(model: model)
        .tabItem { Label("Channels", systemImage: "play.square.stack.fill") }

      ChildSearchView(model: model)
        .tabItem { Label("Search", systemImage: "magnifyingglass") }
    }
    .tint(WatchTheme.cyan)
  }
}
