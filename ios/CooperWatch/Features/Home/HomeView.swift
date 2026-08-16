import SwiftUI

struct HomeView: View {
  @Bindable var model: ChildAppModel

  var body: some View {
    NavigationStack {
      ScrollView {
        if model.feed.isEmpty {
          ContentUnavailableView(
            "Your shelf is ready",
            systemImage: "sparkles.tv",
            description: Text("New videos from your approved channels will show up here.")
          )
          .containerRelativeFrame(.vertical, alignment: .center)
        } else {
          VideoGrid(videos: model.feed, model: model)
            .padding()
        }
      }
      .refreshable { await model.loadLibrary() }
      .navigationTitle("Hey, \(model.profile?.name ?? "there")!")
      .toolbar {
        ToolbarItemGroup(placement: .topBarTrailing) {
          NavigationLink {
            ChildRequestsView(model: model)
          } label: {
            Image(systemName: "bell.badge.fill")
          }
          .accessibilityLabel("My requests")
          Menu {
            Button("Pair a different device", role: .destructive) { model.unpair() }
          } label: {
            Image(systemName: "person.crop.circle.fill")
              .font(.title2).foregroundStyle(WatchTheme.purple)
          }
          .accessibilityLabel("Profile options")
        }
      }
      .watchBackground()
    }
  }
}
