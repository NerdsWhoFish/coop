import SwiftUI

struct HomeView: View {
  @Bindable var model: ChildAppModel

  var body: some View {
    NavigationStack {
      ScrollView {
        if model.feed.isEmpty && model.discoveries.isEmpty {
          ContentUnavailableView(
            "Your shelf is ready",
            systemImage: "sparkles.tv",
            description: Text("New videos from your approved channels will show up here.")
          )
          .containerRelativeFrame(.vertical, alignment: .center)
        } else {
          VStack(alignment: .leading, spacing: 30) {
            VideoGrid(videos: Array(model.feed.prefix(9)), model: model)
            DiscoveryShelf(
              title: "Discover something new",
              discoveries: Array(model.discoveries.prefix(3)),
              model: model
            )
            VideoGrid(videos: Array(model.feed.dropFirst(9)), model: model)
          }
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
              .disabled(model.profile?.allowSelfUnpair != true)
            if model.profile?.allowSelfUnpair != true {
              Text("A parent must enable re-pairing for this device.")
            }
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
