import CoopKit
import SwiftUI

struct SubscriptionsView: View {
  @Bindable var model: ChildAppModel

  var body: some View {
    NavigationStack {
      List(model.subscriptions, id: \.id) { channel in
        NavigationLink {
          ChannelPageView(channelID: channel.id, model: model)
        } label: {
          HStack(spacing: 14) {
            AsyncImage(url: channel.thumbnailUrl.flatMap(URL.init(string:))) { image in
              image.resizable().scaledToFill()
            } placeholder: {
              Image(systemName: "play.rectangle.fill").foregroundStyle(WatchTheme.muted)
            }
            .frame(width: 54, height: 54).clipShape(.circle)
            Text(channel.title).font(.title3.bold())
          }
          .padding(.vertical, 5)
        }
        .listRowBackground(WatchTheme.surface)
      }
      .scrollContentBackground(.hidden)
      .overlay {
        if model.subscriptions.isEmpty {
          ContentUnavailableView(
            "No favorite channels yet",
            systemImage: "play.square.stack",
            description: Text("Open an approved channel and tap Subscribe.")
          )
          .allowsHitTesting(false)
        }
      }
      .refreshable { await model.loadLibrary() }
      .navigationTitle("My channels")
      .watchBackground()
    }
  }
}
