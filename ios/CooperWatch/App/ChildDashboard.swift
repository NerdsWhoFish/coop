import SwiftUI

struct ChildDashboard: View {
  private enum Tab: Hashable {
    case home
    case shorts
    case channels
    case search
  }

  @Bindable var model: ChildAppModel
  @State private var selectedTab: Tab

  init(model: ChildAppModel) {
    self.model = model
    let requested = ProcessInfo.processInfo.environment["COOP_UI_TAB"]
    let initialTab: Tab
    switch requested {
    case "shorts": initialTab = .shorts
    case "channels": initialTab = .channels
    case "search": initialTab = .search
    default: initialTab = .home
    }
    _selectedTab = State(initialValue: initialTab)
  }

  var body: some View {
    TabView(selection: $selectedTab) {
      HomeView(model: model)
        .tag(Tab.home)
        .tabItem { Label("Home", systemImage: "house.fill") }

      if model.profile?.shortsEnabled == true {
        ShortsFeedView(model: model)
          .tag(Tab.shorts)
          .tabItem { Label("Shorts", systemImage: "bolt.fill") }
      }

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
