import CoopKit
import SwiftUI

struct WatchPageView: View {
  @Environment(\.dismiss) private var dismiss
  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  @Environment(\.playbackSurfaceActive) private var playbackSurfaceActive
  @Environment(\.scenePhase) private var scenePhase
  let videoID: String
  @Bindable var model: ChildAppModel
  @State private var page: Components.Schemas.WatchPage?
  @State private var loadFailure: CoopConnectionProblem?
  @State private var watchNext: [Components.Schemas.Video] = []
  @State private var tappedVideo: TappedVideo?
  @State private var tappedChannel: TappedChannel?
  @State private var playerSession = YouTubeEmbeddedPlayerSession()
  @State private var reaction: ChildReaction?
  @State private var startedAt: Date?
  @State private var landscapeBrowsing = false
  @State private var channelSubscribed = false
  @State private var subscriptionIsWorking = false
  @State private var playerIsReady = false

  var body: some View {
    GeometryReader { proxy in
      let isLandscape = proxy.size.width > proxy.size.height

      Group {
        if isLandscape, !landscapeBrowsing {
          landscapeContent(proxy: proxy)
        } else if isLandscape {
          landscapeBrowseContent(proxy: proxy)
        } else {
          portraitContent
        }
      }
      .toolbar(isLandscape ? .hidden : .visible, for: .navigationBar)
      .toolbar(isLandscape ? .hidden : .visible, for: .tabBar)
      .statusBarHidden(isLandscape && !landscapeBrowsing)
      .persistentSystemOverlays(isLandscape && !landscapeBrowsing ? .hidden : .automatic)
      .onChange(of: isLandscape) { _, landscape in
        if !landscape { landscapeBrowsing = false }
      }
    }
    .navigationTitle("Now watching")
    .navigationBarTitleDisplayMode(.inline)
    .navigationDestination(item: $tappedVideo) { tapped in
      WatchPageView(videoID: tapped.id, model: model)
    }
    .navigationDestination(item: $tappedChannel) { tapped in
      ChannelPageView(
        channelID: tapped.id,
        promptedByVideoID: tapped.promptedByVideoID,
        model: model
      )
    }
    .task { await load() }
    .task(id: playbackTaskID) { await maintainPlaybackLease() }
    .onAppear {
      syncPlaybackVisibility()
      model.playbackDidStart()
    }
    .onChange(of: playbackSurfaceActive) { _, _ in syncPlaybackVisibility() }
    .onChange(of: scenePhase) { _, _ in syncPlaybackVisibility() }
    .onDisappear {
      CooperWatchOrientationDelegate.setRegularVideoPlaybackActive(false)
      playerSession.stop()
      Task { _ = await model.updatePlayback(videoID: videoID, state: .stopped) }
      recordWatch()
      model.playbackDidStop()
    }
    .watchBackground()
  }

  @ViewBuilder
  private func landscapeContent(proxy: GeometryProxy) -> some View {
    ZStack {
      Color.black.ignoresSafeArea()
      if let page {
        player(for: page)
          .frame(maxWidth: .infinity, maxHeight: .infinity)
          .ignoresSafeArea()
      } else {
        LoadingOrFailure(failure: loadFailure, retry: load)
      }
      VStack {
        Spacer()
        Button {
          withAnimation(reduceMotion ? nil : .spring(duration: 0.35, bounce: 0.12)) {
            landscapeBrowsing = true
          }
        } label: {
          Label("Swipe from the edge to browse", systemImage: "chevron.left")
            .font(.caption.weight(.bold))
            .padding(.horizontal, 16)
            .padding(.vertical, 9)
            .background(.black.opacity(0.72), in: .capsule)
        }
        .foregroundStyle(.white)
        .accessibilityIdentifier("landscape-browse-button")
        .padding(.bottom, 8)
      }
    }
    .contentShape(.rect)
    .simultaneousGesture(
      DragGesture(minimumDistance: 30).onEnded { value in
        // Only from the right edge: the player's own recommendation rows
        // scroll horizontally, and a whole-screen swipe would hijack them.
        guard value.startLocation.x > proxy.size.width - 44,
          value.translation.width < -60,
          abs(value.translation.width) > abs(value.translation.height)
        else { return }
        withAnimation(reduceMotion ? nil : .spring(duration: 0.35, bounce: 0.12)) {
          landscapeBrowsing = true
        }
      }
    )
  }

  private func landscapeBrowseContent(proxy: GeometryProxy) -> some View {
    ScrollView {
      if let page {
        VStack(alignment: .leading, spacing: 14) {
          HStack {
            Button {
              dismiss()
            } label: {
              Label("Back", systemImage: "chevron.left")
            }
            .buttonStyle(.bordered)

            Spacer()

            Button {
              withAnimation(reduceMotion ? nil : .spring(duration: 0.3, bounce: 0.1)) {
                landscapeBrowsing = false
              }
            } label: {
              Label("Full screen", systemImage: "arrow.up.left.and.arrow.down.right")
            }
            .buttonStyle(.borderedProminent)
          }

          HStack(alignment: .top, spacing: 18) {
            VStack(alignment: .leading, spacing: 12) {
              player(for: page)
                .aspectRatio(16 / 9, contentMode: .fit)
                .clipShape(.rect(cornerRadius: 14))
              Text(page.video.title).font(.headline).lineLimit(2)
              channelLink(for: page)
              reactionControls(for: page)
            }
            .frame(width: proxy.size.width * 0.56)

            VStack(alignment: .leading, spacing: 12) {
              Text("Watch next")
                .font(.title3.bold())
                .accessibilityIdentifier("landscape-watch-next-heading")
              VideoGrid(
                videos: watchNext,
                model: model,
                accessibilityPrefix: "landscape-watch-next",
                style: .list
              )
              DiscoveryShelf(
                title: "Explore next",
                discoveries: model.discoverNext(excluding: page.video.id),
                model: model
              )
            }
            .frame(maxWidth: .infinity, alignment: .leading)
          }
        }
        .padding(18)
        .padding(.bottom, 60)
      } else {
        LoadingOrFailure(failure: loadFailure, retry: load).padding(.top, 40)
      }
    }
    .accessibilityIdentifier("landscape-browse-view")
  }

  private var portraitContent: some View {
    ScrollView {
      if let page {
        VStack(alignment: .leading, spacing: 18) {
          player(for: page)
            .aspectRatio(16 / 9, contentMode: .fit)
            .clipShape(.rect(cornerRadius: 16))

          Text(page.video.title).font(.title2.bold())
          channelLink(for: page)
          reactionControls(for: page)

          if !watchNext.isEmpty {
            VStack(alignment: .leading, spacing: 14) {
              Text("Watch next")
                .font(.title2.bold())
                .accessibilityIdentifier("watch-next-heading")

              VideoGrid(
                videos: watchNext,
                model: model,
                accessibilityPrefix: "watch-next",
                style: .list
              )
            }
            .padding(.top, 8)
          }

          DiscoveryShelf(
            title: "Explore next",
            discoveries: model.discoverNext(excluding: page.video.id),
            model: model
          )
          .padding(.top, 8)
        }
        .padding()
        .padding(.bottom, 84)
      } else {
        LoadingOrFailure(failure: loadFailure, retry: load).padding(.top, 80)
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
      .overlay {
        if model.showsPlayerLoadingPreview {
          VideoLoadingPlaceholder(
            thumbnailURL: page.video.thumbnailUrl.flatMap(URL.init(string:)),
            accessibilityIdentifier: "regular-video-loading"
          )
        }
      }
    } else if let url = URL(string: page.embedUrl), let origin = model.playbackOrigin {
      ZStack {
        YouTubeEmbeddedPlayer(
          url: url,
          origin: origin,
          session: playerSession,
          onVideoLink: { openTappedVideo($0) },
          onReady: {
            withAnimation(reduceMotion ? nil : .easeOut(duration: 0.22)) {
              playerIsReady = true
            }
          }
        )

        if !playerIsReady {
          VideoLoadingPlaceholder(
            thumbnailURL: page.video.thumbnailUrl.flatMap(URL.init(string:)),
            accessibilityIdentifier: "regular-video-loading"
          )
          .transition(.opacity)
        }
      }
      .accessibilityIdentifier("regular-video-player")
    }
  }

  // A tap inside the player navigates when Coop would serve the video, and
  // otherwise lands on the channel page so the child can ask for it there.
  private func openTappedVideo(_ id: String) {
    guard id != videoID else { return }
    Task {
      if (try? await model.video(id: id)) != nil {
        tappedVideo = TappedVideo(id: id)
      } else if let channelID = await model.channelForVideo(id: id) {
        tappedChannel = TappedChannel(id: channelID, promptedByVideoID: id)
      } else {
        model.errorMessage = "That video isn't in your Coop yet."
      }
    }
  }

  private func load() async {
    do {
      page = try await model.video(id: videoID)
      if let page {
        reaction = page.reaction == .like ? .like : (page.reaction == .dislike ? .dislike : nil)
        channelSubscribed = model.isSubscribed(to: page.video.channelId)
        startedAt = .now
      }
      loadFailure = nil
    } catch {
      // Stays on this page: an alert plus a dead spinner loses the video the
      // child picked, and they have no way back to it.
      loadFailure = CoopConnectionProblem(error)
    }
    watchNext = await model.watchNext(excluding: videoID)
  }

  private func reactionButton(_ value: ChildReaction, label: String, symbol: String) -> some View {
    Button {
      let newValue: ChildReaction? = reaction == value ? nil : value
      reaction = newValue
      Task {
        do { try await model.setReaction(newValue, videoID: videoID) } catch {
          model.report(error)
        }
      }
    } label: {
      Label(label, systemImage: symbol).frame(maxWidth: .infinity)
    }
    .buttonStyle(.borderedProminent)
    .tint(reaction == value ? WatchTheme.pink : WatchTheme.surface)
  }

  private func channelLink(for page: Components.Schemas.WatchPage) -> some View {
    NavigationLink {
      ChannelPageView(channelID: page.video.channelId, model: model)
    } label: {
      Label(page.video.channelTitle ?? "Channel", systemImage: "play.square.stack.fill")
        .font(.headline)
        .foregroundStyle(WatchTheme.cyan)
    }
  }

  private func reactionControls(for page: Components.Schemas.WatchPage) -> some View {
    Grid(horizontalSpacing: 10, verticalSpacing: 10) {
      GridRow {
        reactionButton(.like, label: "Like", symbol: "hand.thumbsup.fill")
        reactionButton(.dislike, label: "Not for me", symbol: "hand.thumbsdown.fill")
      }

      GridRow {
        Button {
          setSubscribed(!channelSubscribed, channelID: page.video.channelId)
        } label: {
          Label(
            channelSubscribed ? "Subscribed" : "Subscribe",
            systemImage: channelSubscribed ? "checkmark" : "plus"
          )
          .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .tint(channelSubscribed ? WatchTheme.green : WatchTheme.cyan)
        .foregroundStyle(WatchTheme.background)
        .disabled(subscriptionIsWorking)

        NavigationLink {
          ChannelPageView(channelID: page.video.channelId, model: model)
        } label: {
          Label("Channel", systemImage: "play.square.stack.fill")
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.bordered)
      }

      GridRow {
        if let share = page.shareUrl.flatMap(URL.init(string:)) {
          ShareLink(item: share) {
            Label("Share", systemImage: "square.and.arrow.up").frame(maxWidth: .infinity)
          }
          .buttonStyle(.bordered)
        }
        Color.clear
      }
    }
    .frame(maxWidth: .infinity)
    .font(.caption.weight(.semibold))
    .lineLimit(1)
    .minimumScaleFactor(0.72)
  }

  private func setSubscribed(_ subscribed: Bool, channelID: String) {
    channelSubscribed = subscribed
    subscriptionIsWorking = true
    Task {
      do {
        try await model.setSubscribed(subscribed, channelID: channelID)
      } catch {
        channelSubscribed.toggle()
        model.report(error)
      }
      subscriptionIsWorking = false
    }
  }

  private func recordWatch() {
    guard let startedAt else { return }
    self.startedAt = nil
    let seconds = max(0, Int(Date.now.timeIntervalSince(startedAt)))
    Task {
      await model.recordWatch(videoID: videoID, startedAt: startedAt, secondsWatched: seconds)
    }
  }

  private func maintainPlaybackLease() async {
    guard page != nil, isPlaybackActive else { return }
    guard await model.updatePlayback(videoID: videoID, state: .started) else {
      dismiss()
      return
    }
    do {
      while !Task.isCancelled {
        try await Task.sleep(for: .seconds(15))
        guard await model.updatePlayback(videoID: videoID, state: .heartbeat) else {
          playerSession.stop()
          await model.loadLibrary()
          dismiss()
          return
        }
      }
    } catch is CancellationError {
      return
    } catch {
      return
    }
  }

  private var isPlaybackActive: Bool {
    // Inactive is not background: Control Center or a notification pulled over
    // the app must not stop and restart the video.
    playbackSurfaceActive && scenePhase != .background
  }

  private var playbackTaskID: String {
    "\(page?.video.id ?? "loading")-\(isPlaybackActive)"
  }

  private func syncPlaybackVisibility() {
    CooperWatchOrientationDelegate.setRegularVideoPlaybackActive(isPlaybackActive)
    if isPlaybackActive {
      if page != nil, startedAt == nil { startedAt = .now }
      return
    }
    playerSession.stop()
    Task { _ = await model.updatePlayback(videoID: videoID, state: .stopped) }
    recordWatch()
  }
}

/// A video the child tapped inside the embedded player.
struct TappedVideo: Identifiable, Hashable {
  let id: String
}

/// The channel behind an unwatchable tapped video, headed for the ask flow.
struct TappedChannel: Identifiable, Hashable {
  let id: String
  let promptedByVideoID: String
}
