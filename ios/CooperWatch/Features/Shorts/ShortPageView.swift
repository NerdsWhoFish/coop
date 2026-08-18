import CoopKit
import SwiftUI

struct ShortPageView: View {
  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  let video: Components.Schemas.Video
  let isActive: Bool
  @Bindable var model: ChildAppModel
  let onBlocked: () -> Void
  @State private var page: Components.Schemas.WatchPage?
  @State private var reaction: ChildReaction?
  @State private var isFetching = false
  @State private var isUpdatingReaction = false
  @State private var loadError: String?
  @State private var startedAt: Date?
  @State private var playerIsReady = false
  @State private var tappedVideo: TappedVideo?
  @State private var tappedChannel: TappedChannel?

  private var accent: Color {
    let colors = [WatchTheme.cyan, WatchTheme.pink, WatchTheme.purple, WatchTheme.green]
    let seed = video.id.unicodeScalars.reduce(0) { $0 + Int($1.value) }
    return colors[seed % colors.count]
  }

  var body: some View {
    GeometryReader { proxy in
      VStack(spacing: 0) {
        player
          .frame(width: proxy.size.width)
          .frame(maxHeight: .infinity)
        if isActive {
          actionBar
        } else {
          Color.clear.frame(height: 64)
        }
      }
      .frame(width: proxy.size.width, height: proxy.size.height)
      .clipped()
    }
    .task(id: isActive) {
      if isActive {
        await preparePlayback()
        await maintainPlaybackLease()
      }
    }
    .onChange(of: isActive) { _, active in
      playerIsReady = false
      if active {
        startWatchingIfReady()
      } else {
        stopPlaybackLease()
        recordWatch()
      }
    }
    .onDisappear {
      stopPlaybackLease()
      recordWatch()
    }
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
    .animation(reduceMotion ? nil : .spring(duration: 0.42, bounce: 0.12), value: isActive)
  }

  private func openTappedVideo(_ id: String) {
    guard id != video.id else { return }
    Task {
      if (try? await model.video(id: id)) != nil {
        tappedVideo = TappedVideo(id: id)
      } else if let channelID = await model.channelForVideo(id: id) {
        tappedChannel = TappedChannel(id: channelID, promptedByVideoID: id)
      } else {
        loadError = "That video isn't in your Coop yet."
      }
    }
  }

  private var player: some View {
    ZStack {
      ShortArtwork(video: video, accent: accent)

      if isActive, model.isPreviewMode {
        ShortPreviewPlayer(accent: accent)
          .accessibilityHidden(true)
        Rectangle()
          .fill(.black.opacity(0.001))
          .allowsHitTesting(false)
          .accessibilityElement(children: .ignore)
          .accessibilityLabel("Playing \(video.title)")
          .accessibilityIdentifier("active-short-player")
        if model.showsPlayerLoadingPreview {
          VideoLoadingPlaceholder(
            thumbnailURL: video.thumbnailUrl.flatMap(URL.init(string:)),
            accessibilityIdentifier: "short-video-loading"
          )
        }
      } else if isActive, let playerURL, let origin = model.playbackOrigin {
        YouTubeEmbeddedPlayer(
          url: playerURL,
          origin: origin,
          onVideoLink: { openTappedVideo($0) },
          onReady: {
            withAnimation(reduceMotion ? nil : .easeOut(duration: 0.22)) {
              playerIsReady = true
            }
          }
        )
        .transition(.opacity)

        if !playerIsReady {
          VideoLoadingPlaceholder(
            thumbnailURL: video.thumbnailUrl.flatMap(URL.init(string:)),
            accessibilityIdentifier: "short-video-loading"
          )
          .transition(.opacity)
        }
      }

      if isActive, isFetching {
        ProgressView()
          .controlSize(.large)
          .padding(18)
          .background(.black.opacity(0.7), in: .circle)
      }

      if isActive, loadError != nil {
        VStack(spacing: 12) {
          Image(systemName: "exclamationmark.arrow.trianglehead.2.clockwise.rotate.90")
            .font(.largeTitle)
          Text("This one got tangled").font(.headline)
          Button("Try again") { Task { await retryPlayback() } }
            .buttonStyle(.borderedProminent)
            .tint(accent)
        }
        .padding(22)
        .background(.black.opacity(0.82), in: .rect(cornerRadius: 20))
      }

      if isActive {
        NavigationLink {
          ChannelPageView(channelID: video.channelId, model: model)
        } label: {
          Label(video.channelTitle ?? "Channel", systemImage: "play.square.stack.fill")
            .font(.subheadline.weight(.bold))
            .lineLimit(1)
            .padding(.horizontal, 12)
            .padding(.vertical, 9)
            .background(.black.opacity(0.72), in: .capsule)
            .foregroundStyle(.white)
        }
        .buttonStyle(.plain)
        .padding(12)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomLeading)
        .accessibilityIdentifier("short-channel-link")
      }
    }
    .background(.black)
  }

  private var actionBar: some View {
    ZStack {
      Rectangle()
        .fill(WatchTheme.background)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Short actions")
        .accessibilityIdentifier(isActive ? "active-short-action-bar" : "short-action-bar")
      HStack(spacing: 12) {
        reactionButton(.like, symbol: "hand.thumbsup.fill", label: "Like")
        reactionButton(.dislike, symbol: "hand.thumbsdown.fill", label: "Not for me")
        if let shareURL = page?.shareUrl.flatMap(URL.init(string:)) {
          ShareLink(item: shareURL) {
            actionLabel(symbol: "square.and.arrow.up", label: "Share")
          }
        } else {
          actionLabel(symbol: "square.and.arrow.up", label: "Share")
            .opacity(0.35)
        }
      }
      .frame(maxWidth: 260)
      .disabled(page == nil || isUpdatingReaction)
    }
    .frame(height: 64)
    .foregroundStyle(WatchTheme.foreground)
    .overlay(alignment: .top) {
      Rectangle().fill(WatchTheme.foreground.opacity(0.12)).frame(height: 1)
    }
  }

  private var playerURL: URL? {
    guard let page else { return nil }
    return ShortPlaybackURL.make(baseURL: page.embedUrl, videoID: video.id)
  }

  private func reactionButton(_ value: ChildReaction, symbol: String, label: String) -> some View {
    Button {
      updateReaction(value)
    } label: {
      actionLabel(
        symbol: symbol,
        label: label,
        background: reaction == value ? accent : WatchTheme.surface
      )
    }
    .foregroundStyle(reaction == value ? WatchTheme.background : WatchTheme.foreground)
    .accessibilityValue(reaction == value ? "Selected" : "Not selected")
  }

  private func actionLabel(
    symbol: String,
    label: String,
    background: Color = WatchTheme.surface
  ) -> some View {
    Image(systemName: symbol)
      .font(.title3.weight(.bold))
      .frame(width: 48, height: 48)
      .background(background, in: .rect(cornerRadius: 14))
      .accessibilityLabel(label)
  }

  private func preparePlayback() async {
    if page != nil {
      startWatchingIfReady()
      return
    }
    isFetching = true
    loadError = nil
    defer { isFetching = false }
    do {
      page = try await model.video(id: video.id)
      guard !Task.isCancelled, isActive else { return }
      if let page {
        reaction = page.reaction == .like ? .like : (page.reaction == .dislike ? .dislike : nil)
        startWatchingIfReady()
      } else {
        loadError = "The video is unavailable."
      }
    } catch {
      guard !Task.isCancelled else { return }
      loadError = error.localizedDescription
    }
  }

  private func retryPlayback() async {
    page = nil
    await preparePlayback()
  }

  private func startWatchingIfReady() {
    guard isActive, page != nil, startedAt == nil else { return }
    startedAt = .now
  }

  private func recordWatch() {
    guard let startedAt else { return }
    self.startedAt = nil
    let seconds = max(0, Int(Date.now.timeIntervalSince(startedAt)))
    Task {
      await model.recordWatch(videoID: video.id, startedAt: startedAt, secondsWatched: seconds)
    }
  }

  private func maintainPlaybackLease() async {
    guard isActive, page != nil else { return }
    guard await model.updatePlayback(videoID: video.id, state: .started) else {
      onBlocked()
      return
    }
    do {
      while !Task.isCancelled, isActive {
        try await Task.sleep(for: .seconds(15))
        guard await model.updatePlayback(videoID: video.id, state: .heartbeat) else {
          recordWatch()
          onBlocked()
          return
        }
      }
    } catch is CancellationError {
      return
    } catch {
      return
    }
  }

  private func stopPlaybackLease() {
    Task { _ = await model.updatePlayback(videoID: video.id, state: .stopped) }
  }

  private func updateReaction(_ value: ChildReaction) {
    guard !isUpdatingReaction else { return }
    let previous = reaction
    let updated: ChildReaction? = reaction == value ? nil : value
    reaction = updated
    isUpdatingReaction = true
    Task {
      do {
        try await model.setReaction(updated, videoID: video.id)
      } catch {
        reaction = previous
        model.errorMessage = error.localizedDescription
      }
      isUpdatingReaction = false
    }
  }
}

private struct ShortArtwork: View {
  let video: Components.Schemas.Video
  let accent: Color

  var body: some View {
    ZStack {
      LinearGradient(
        colors: [accent.opacity(0.72), WatchTheme.purple.opacity(0.36), .black],
        startPoint: .topLeading,
        endPoint: .bottomTrailing
      )
      AsyncImage(url: video.thumbnailUrl.flatMap(URL.init(string:))) { image in
        image.resizable().scaledToFit()
      } placeholder: {
        Image(systemName: "bolt.fill")
          .font(.system(size: 72, weight: .black))
          .foregroundStyle(WatchTheme.foreground.opacity(0.76))
      }
    }
    .clipped()
  }
}

private struct ShortPreviewPlayer: View {
  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  let accent: Color
  @State private var pulse = false

  var body: some View {
    ZStack {
      LinearGradient(
        colors: [.black, accent.opacity(0.72), WatchTheme.purple],
        startPoint: .top,
        endPoint: .bottomTrailing
      )
      Circle()
        .stroke(WatchTheme.foreground.opacity(0.28), lineWidth: 18)
        .scaleEffect(pulse ? 1.35 : 0.72)
        .opacity(pulse ? 0.08 : 0.5)
      VStack(spacing: 14) {
        Image(systemName: "bolt.fill")
          .font(.system(size: 82, weight: .black))
        Text("PLAYING")
          .font(.caption.weight(.black))
          .tracking(3)
      }
      .foregroundStyle(WatchTheme.foreground)
    }
    .onAppear {
      guard !reduceMotion else { return }
      withAnimation(.easeInOut(duration: 1.3).repeatForever(autoreverses: true)) {
        pulse = true
      }
    }
  }
}
