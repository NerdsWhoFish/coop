import CoopKit
import SwiftUI

struct WatchPageView: View {
  let videoID: String
  @Bindable var model: ChildAppModel
  @State private var page: Components.Schemas.WatchPage?
  @State private var playerLoaded = false
  @State private var reaction: ChildReaction?
  @State private var startedAt: Date?

  var body: some View {
    GeometryReader { proxy in
      let isLandscape = proxy.size.width > proxy.size.height

      Group {
        if isLandscape {
          landscapeContent
        } else {
          portraitContent
        }
      }
      .toolbar(isLandscape ? .hidden : .visible, for: .navigationBar)
      .toolbar(isLandscape ? .hidden : .visible, for: .tabBar)
      .statusBarHidden(isLandscape)
      .persistentSystemOverlays(isLandscape ? .hidden : .automatic)
    }
    .navigationTitle("Now watching")
    .navigationBarTitleDisplayMode(.inline)
    .task { await load() }
    .onAppear { CooperWatchOrientationDelegate.setRegularVideoPlaybackActive(true) }
    .onDisappear {
      CooperWatchOrientationDelegate.setRegularVideoPlaybackActive(false)
      recordWatch()
    }
    .watchBackground()
  }

  @ViewBuilder
  private var landscapeContent: some View {
    ZStack {
      Color.black.ignoresSafeArea()
      if let page {
        player(for: page)
          .ignoresSafeArea()
      } else {
        ProgressView()
      }
    }
  }

  private var portraitContent: some View {
    ScrollView {
      if let page {
        VStack(alignment: .leading, spacing: 18) {
          player(for: page)
            .aspectRatio(16 / 9, contentMode: .fit)
            .clipShape(.rect(cornerRadius: 16))

          Text(page.video.title).font(.title2.bold())
          NavigationLink {
            ChannelPageView(channelID: page.video.channelId, model: model)
          } label: {
            Label(page.video.channelTitle ?? "Channel", systemImage: "play.square.stack.fill")
              .font(.headline).foregroundStyle(WatchTheme.cyan)
          }

          HStack(spacing: 12) {
            reactionButton(.like, label: "Like", symbol: "hand.thumbsup.fill")
            reactionButton(.dislike, label: "Not for me", symbol: "hand.thumbsdown.fill")
            if let share = page.shareUrl.flatMap(URL.init(string:)) {
              ShareLink(item: share) {
                Label("Share", systemImage: "square.and.arrow.up").frame(maxWidth: .infinity)
              }
              .buttonStyle(.bordered)
            }
          }
        }
        .padding()
      } else {
        ProgressView().padding(.top, 80)
      }
    }
    .refreshable { await load() }
  }

  @ViewBuilder
  private func player(for page: Components.Schemas.WatchPage) -> some View {
    if model.isPreviewMode {
      ZStack {
        Color.black
        Image(systemName: "play.rectangle.fill")
          .font(.system(size: 72, weight: .bold))
          .foregroundStyle(WatchTheme.cyan)
      }
      .accessibilityElement(children: .ignore)
      .accessibilityLabel("Playing \(page.video.title)")
      .accessibilityIdentifier("regular-video-player")
    } else if playerLoaded, let url = URL(string: page.embedUrl) {
      YouTubeEmbeddedPlayer(url: url)
    } else {
      Button {
        loadPlayer()
      } label: {
        ZStack {
          AsyncImage(url: page.video.thumbnailUrl.flatMap(URL.init(string:))) { image in
            image.resizable().scaledToFill()
          } placeholder: {
            Rectangle().fill(WatchTheme.surface)
          }
          Image(systemName: "play.circle.fill")
            .font(.system(size: 70)).foregroundStyle(WatchTheme.foreground, WatchTheme.pink)
            .shadow(radius: 12)
        }
      }
      .buttonStyle(.plain)
    }
  }

  private func load() async {
    do {
      page = try await model.video(id: videoID)
      if let page {
        reaction = page.reaction == .like ? .like : (page.reaction == .dislike ? .dislike : nil)
        if page.autoplay { loadPlayer() }
      }
    } catch { model.errorMessage = error.localizedDescription }
  }

  private func loadPlayer() {
    startedAt = .now
    playerLoaded = true
  }

  private func reactionButton(_ value: ChildReaction, label: String, symbol: String) -> some View {
    Button {
      let newValue: ChildReaction? = reaction == value ? nil : value
      reaction = newValue
      Task {
        do { try await model.setReaction(newValue, videoID: videoID) } catch {
          model.errorMessage = error.localizedDescription
        }
      }
    } label: {
      Label(label, systemImage: symbol).frame(maxWidth: .infinity)
    }
    .buttonStyle(.borderedProminent)
    .tint(reaction == value ? WatchTheme.pink : WatchTheme.surface)
  }

  private func recordWatch() {
    guard let startedAt else { return }
    let seconds = max(0, Int(Date.now.timeIntervalSince(startedAt)))
    Task {
      await model.recordWatch(videoID: videoID, startedAt: startedAt, secondsWatched: seconds)
    }
  }
}
