import CoopKit
import SwiftUI

struct ChannelPageView: View {
  let channelID: String
  @Bindable var model: ChildAppModel
  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  @State private var page: Components.Schemas.ChannelPage?
  @State private var isWorking = false
  @State private var asked = false

  var body: some View {
    ScrollView {
      if let page {
        VStack(spacing: 20) {
          if let banner = page.channel.bannerUrl.flatMap(URL.init(string:)) {
            AsyncImage(url: banner) { image in
              image.resizable().scaledToFill()
            } placeholder: {
              WatchTheme.surface
            }
            .frame(height: 150).clipped()
          }

          HStack(spacing: 14) {
            AsyncImage(url: page.channel.thumbnailUrl.flatMap(URL.init(string:))) { image in
              image.resizable().scaledToFill()
            } placeholder: {
              Image(systemName: "play.rectangle.fill")
            }
            .frame(width: 72, height: 72).clipShape(.circle)
            VStack(alignment: .leading, spacing: 4) {
              Text(page.channel.title).font(.title.bold())
              if let count = page.channel.subscriberCount {
                Text("\(count.formatted()) YouTube subscribers")
                  .font(.caption).foregroundStyle(WatchTheme.foreground.opacity(0.62))
              }
            }
            Spacer()
          }

          if page.state == .requestable {
            Button {
              ask(page.channel.id)
            } label: {
              Label(
                asked || (page.pendingRequest ?? false) ? "ASKED" : "Ask to watch",
                systemImage: asked ? "checkmark.seal.fill" : "lock.open.fill"
              )
              .font(.title3.weight(.black)).tracking(asked ? 1.5 : 0)
              .frame(maxWidth: .infinity).padding(.vertical, 8)
            }
            .buttonStyle(.borderedProminent)
            .tint(asked || (page.pendingRequest ?? false) ? WatchTheme.green : WatchTheme.yellow)
            .foregroundStyle(WatchTheme.background)
            .disabled(asked || (page.pendingRequest ?? false) || isWorking)
            .scaleEffect(asked && !reduceMotion ? 1.04 : 1)
          } else {
            Button(
              page.subscribed ? "Following" : "Follow",
              systemImage: page.subscribed ? "checkmark" : "plus"
            ) {
              follow(!page.subscribed)
            }
            .buttonStyle(.borderedProminent).tint(
              page.subscribed ? WatchTheme.green : WatchTheme.cyan
            )
            .foregroundStyle(WatchTheme.background).disabled(isWorking)
          }

          VideoGrid(videos: page.videos, model: model)
        }
        .padding()
      } else {
        ProgressView().padding(.top, 80)
      }
    }
    .navigationTitle(page?.channel.title ?? "Channel")
    .navigationBarTitleDisplayMode(.inline)
    .task { await load() }
    .watchBackground()
  }

  private func load() async {
    do { page = try await model.channel(id: channelID) } catch {
      model.errorMessage = error.localizedDescription
    }
  }

  private func follow(_ subscribed: Bool) {
    isWorking = true
    Task {
      do {
        try await model.setSubscribed(subscribed, channelID: channelID)
        await load()
      } catch { model.errorMessage = error.localizedDescription }
      isWorking = false
    }
  }

  private func ask(_ channelID: String) {
    isWorking = true
    Task {
      do {
        try await model.requestChannel(channelID: channelID)
        withAnimation(reduceMotion ? nil : .spring(duration: 0.3, bounce: 0.35)) { asked = true }
      } catch { model.errorMessage = error.localizedDescription }
      isWorking = false
    }
  }
}
