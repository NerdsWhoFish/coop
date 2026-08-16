import CoopKit
import SwiftUI

struct ShortPageView: View {
  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  @Environment(\.dynamicTypeSize) private var dynamicTypeSize
  let video: Components.Schemas.Video
  let position: Int
  let isFirst: Bool
  let isActive: Bool
  @Bindable var model: ChildAppModel
  @State private var page: Components.Schemas.WatchPage?
  @State private var reaction: ChildReaction?
  @State private var isFetching = false
  @State private var isUpdatingReaction = false
  @State private var loadError: String?
  @State private var startedAt: Date?

  private var accent: Color {
    let colors = [WatchTheme.cyan, WatchTheme.pink, WatchTheme.purple, WatchTheme.green]
    let seed = video.id.unicodeScalars.reduce(0) { $0 + Int($1.value) }
    return colors[seed % colors.count]
  }

  var body: some View {
    GeometryReader { proxy in
      ZStack {
        atmosphericBackground

        if proxy.size.width >= 700 {
          wideLayout(size: proxy.size)
        } else {
          compactLayout(size: proxy.size)
        }
      }
      .frame(width: proxy.size.width, height: proxy.size.height)
      .clipped()
    }
    .dynamicTypeSize(.large ... .accessibility1)
    .task(id: isActive) {
      if isActive {
        await preparePlayback()
      }
    }
    .onChange(of: isActive) { _, active in
      if active {
        startWatchingIfReady()
      } else {
        recordWatch()
      }
    }
    .onDisappear { recordWatch() }
    .animation(reduceMotion ? nil : .spring(duration: 0.42, bounce: 0.12), value: isActive)
  }

  private var atmosphericBackground: some View {
    ZStack {
      WatchTheme.background
      RadialGradient(
        colors: [accent.opacity(isActive ? 0.34 : 0.16), .clear],
        center: .top,
        startRadius: 20,
        endRadius: 520
      )
      LinearGradient(
        colors: [WatchTheme.background.opacity(0), WatchTheme.background],
        startPoint: .top,
        endPoint: .bottom
      )
    }
    .ignoresSafeArea()
  }

  private func compactLayout(size: CGSize) -> some View {
    let fraction = dynamicTypeSize.isAccessibilitySize ? 0.42 : 0.62
    let playerHeight = max(210, min(size.height * fraction, 540))
    let playerWidth = min(size.width - 28, playerHeight * 9 / 16)

    return VStack(spacing: 0) {
      Spacer(minLength: 8)
      player
        .frame(width: playerWidth, height: playerHeight)
      chrome
        .frame(maxWidth: 560)
        .padding(.horizontal, 18)
        .padding(.top, 14)
        .padding(.bottom, 10)
      Spacer(minLength: 0)
    }
  }

  private func wideLayout(size: CGSize) -> some View {
    let playerHeight = min(size.height - 54, 660)

    return HStack(spacing: 40) {
      player
        .frame(width: playerHeight * 9 / 16, height: playerHeight)
      chrome
        .frame(maxWidth: 440, alignment: .leading)
    }
    .padding(.horizontal, 42)
    .frame(maxWidth: .infinity, maxHeight: .infinity)
  }

  private var player: some View {
    ZStack {
      ShortArtwork(video: video, accent: accent)

      if isActive, model.isPreviewMode {
        ShortPreviewPlayer(accent: accent)
      } else if isActive, let playerURL {
        ShortEmbeddedPlayer(url: playerURL)
          .transition(.opacity)
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
    }
    .background(.black)
    .clipShape(.rect(cornerRadius: 24))
    .overlay {
      RoundedRectangle(cornerRadius: 24)
        .stroke(accent.opacity(isActive ? 0.92 : 0.24), lineWidth: isActive ? 3 : 1)
    }
    .shadow(color: accent.opacity(isActive ? 0.36 : 0.08), radius: isActive ? 24 : 8)
    .scaleEffect(isActive || reduceMotion ? 1 : 0.96)
    .accessibilityLabel(isActive ? "Playing \(video.title)" : "Short: \(video.title)")
    .accessibilityIdentifier(isActive ? "active-short-player" : "short-player")
  }

  private var chrome: some View {
    VStack(alignment: .leading, spacing: dynamicTypeSize.isAccessibilitySize ? 10 : 14) {
      HStack {
        Label("SHORT \(position)", systemImage: "bolt.fill")
          .font(.caption.weight(.black))
          .tracking(1.4)
          .foregroundStyle(accent)
        Spacer()
        if isActive {
          Text("NOW PLAYING")
            .font(.caption2.weight(.black))
            .tracking(1.2)
            .foregroundStyle(WatchTheme.green)
            .accessibilityIdentifier("active-short-status")
        }
      }

      Text(video.title)
        .font(.title2.weight(.black))
        .lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 2)

      NavigationLink {
        ChannelPageView(channelID: video.channelId, model: model)
      } label: {
        Label(video.channelTitle ?? "Channel", systemImage: "play.square.stack.fill")
          .font(.headline)
          .foregroundStyle(WatchTheme.cyan)
      }

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
      .disabled(page == nil || isUpdatingReaction)

      if isFirst, !dynamicTypeSize.isAccessibilitySize {
        Label("Swipe up for another", systemImage: "arrow.up")
          .font(.subheadline.weight(.semibold))
          .foregroundStyle(WatchTheme.foreground.opacity(0.64))
      }
    }
    .foregroundStyle(WatchTheme.foreground)
  }

  private var playerURL: URL? {
    guard let page else { return nil }
    return ShortPlaybackURL.make(baseURL: page.embedUrl, videoID: video.id)
  }

  private func reactionButton(_ value: ChildReaction, symbol: String, label: String) -> some View {
    Button {
      updateReaction(value)
    } label: {
      actionLabel(symbol: symbol, label: label)
    }
    .foregroundStyle(reaction == value ? WatchTheme.background : WatchTheme.foreground)
    .background(reaction == value ? accent : WatchTheme.surface, in: .rect(cornerRadius: 14))
    .accessibilityValue(reaction == value ? "Selected" : "Not selected")
  }

  private func actionLabel(symbol: String, label: String) -> some View {
    Image(systemName: symbol)
      .font(.title3.weight(.bold))
      .frame(width: 48, height: 48)
      .background(WatchTheme.surface, in: .rect(cornerRadius: 14))
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
        image.resizable().scaledToFill()
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
