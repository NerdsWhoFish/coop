import CoopKit
import SwiftUI

struct VideoCard: View {
  let video: Components.Schemas.Video
  var locked = false

  var body: some View {
    VStack(alignment: .leading, spacing: 9) {
      VideoThumbnail(video: video, locked: locked)
        .aspectRatio(16 / 9, contentMode: .fit)
        .clipShape(.rect(cornerRadius: 14))

      HStack(alignment: .top, spacing: 11) {
        Image(systemName: "play.square.stack.fill")
          .font(.title2)
          .foregroundStyle(WatchTheme.cyan)
          .frame(width: 34, height: 34)

        VStack(alignment: .leading, spacing: 3) {
          Text(video.title)
            .font(.headline)
            .lineLimit(2)
            .foregroundStyle(WatchTheme.foreground)
          Text(metadata)
            .font(.subheadline)
            .lineLimit(1)
            .foregroundStyle(WatchTheme.foreground.opacity(0.62))
        }
        .frame(maxWidth: .infinity, alignment: .leading)
      }
    }
    .accessibilityElement(children: .combine)
    .accessibilityLabel(locked ? "Locked video, \(video.title)" : video.title)
  }

  private var metadata: String {
    let channel = video.channelTitle ?? "Approved channel"
    guard let publishedAt = video.publishedAt else { return channel }
    return "\(channel) · \(publishedAt.formatted(.relative(presentation: .named)))"
  }
}

private struct VideoThumbnail: View {
  let video: Components.Schemas.Video
  let locked: Bool

  var body: some View {
    ZStack {
      AsyncImage(url: video.thumbnailUrl.flatMap(URL.init(string:))) { image in
        image.resizable().scaledToFill()
      } placeholder: {
        Rectangle().fill(
          LinearGradient(
            colors: [WatchTheme.purple.opacity(0.72), WatchTheme.cyan.opacity(0.42)],
            startPoint: .topLeading,
            endPoint: .bottomTrailing
          )
        )
        .overlay {
          Image(systemName: "play.fill").font(.title).foregroundStyle(WatchTheme.muted)
        }
      }

      if locked {
        ZStack {
          Color.black.opacity(0.5)
          Label("ASK", systemImage: "lock.fill")
            .font(.headline.weight(.black)).tracking(1.2)
            .padding(.horizontal, 14).padding(.vertical, 9)
            .background(WatchTheme.yellow, in: .capsule)
            .foregroundStyle(WatchTheme.background)
        }
      }

      if let duration = video.durationSeconds, duration > 0 {
        Text(formattedDuration(duration))
          .font(.caption.bold().monospacedDigit())
          .foregroundStyle(.white)
          .padding(.horizontal, 5)
          .padding(.vertical, 3)
          .background(.black.opacity(0.78), in: .rect(cornerRadius: 5))
          .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomTrailing)
          .padding(7)
      }
    }
  }

  private func formattedDuration(_ seconds: Int) -> String {
    let hours = seconds / 3_600
    let minutes = (seconds % 3_600) / 60
    let remainingSeconds = seconds % 60
    return hours > 0
      ? String(format: "%d:%02d:%02d", hours, minutes, remainingSeconds)
      : String(format: "%d:%02d", minutes, remainingSeconds)
  }
}

enum VideoCollectionStyle {
  case grid
  case list
}

struct VideoGrid: View {
  let videos: [Components.Schemas.Video]
  let model: ChildAppModel
  var accessibilityPrefix: String?
  var style: VideoCollectionStyle = .grid

  @ViewBuilder
  var body: some View {
    switch style {
    case .grid:
      LazyVGrid(
        columns: [GridItem(.adaptive(minimum: 250, maximum: 420), spacing: 20)],
        spacing: 24
      ) {
        links()
      }
    case .list:
      LazyVStack(spacing: 22) {
        links()
      }
    }
  }

  @ViewBuilder
  private func links() -> some View {
    ForEach(videos, id: \.id) { video in
      NavigationLink {
        WatchPageView(videoID: video.id, model: model)
      } label: {
        VideoCard(video: video)
      }
      .buttonStyle(.plain)
      .accessibilityIdentifier(
        accessibilityPrefix.map { "\($0)-\(video.id)" } ?? "video-\(video.id)"
      )
    }
  }
}
