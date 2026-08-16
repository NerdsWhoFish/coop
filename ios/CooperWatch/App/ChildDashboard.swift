import SwiftUI

struct ChildDashboard: View {
  private enum Tab: Hashable {
    case home
    case channels
    case search
  }

  @Bindable var model: ChildAppModel
  @State private var selectedTab: Tab

  init(model: ChildAppModel) {
    self.model = model
    let requested = ProcessInfo.processInfo.environment["COOP_UI_TAB"]
    _selectedTab = State(
      initialValue: requested == "channels" ? .channels : (requested == "search" ? .search : .home)
    )
  }

  var body: some View {
    TabView(selection: $selectedTab) {
      HomeView(model: model)
        .tag(Tab.home)
        .tabItem { Label("Home", systemImage: "house.fill") }

      SubscriptionsView(model: model)
        .tag(Tab.channels)
        .tabItem { Label("Channels", systemImage: "play.square.stack.fill") }

      ChildSearchView(model: model)
        .tag(Tab.search)
        .tabItem { Label("Search", systemImage: "magnifyingglass") }
    }
    .tint(WatchTheme.cyan)
  }
}
