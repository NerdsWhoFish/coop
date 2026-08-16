import CoopKit
import SwiftUI

struct VideoCard: View {
  let video: Components.Schemas.Video
  var locked = false

  var body: some View {
    VStack(alignment: .leading, spacing: 9) {
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
            Image(systemName: "play.fill").font(.largeTitle).foregroundStyle(WatchTheme.muted)
          }
        }
        .aspectRatio(16 / 9, contentMode: .fit)
        .clipShape(.rect(cornerRadius: 14))

        if locked {
          ZStack {
            Color.black.opacity(0.5)
            Label("ASK", systemImage: "lock.fill")
              .font(.headline.weight(.black)).tracking(1.2)
              .padding(.horizontal, 14).padding(.vertical, 9)
              .background(WatchTheme.yellow, in: .capsule)
              .foregroundStyle(WatchTheme.background)
          }
          .clipShape(.rect(cornerRadius: 14))
        }
      }

      Text(video.title)
        .font(.headline).lineLimit(2).foregroundStyle(WatchTheme.foreground)
      Text(video.channelTitle ?? "")
        .font(.subheadline).lineLimit(1).foregroundStyle(WatchTheme.foreground.opacity(0.62))
    }
    .accessibilityElement(children: .combine)
    .accessibilityLabel(locked ? "Locked video, \(video.title)" : video.title)
  }
}

struct VideoGrid: View {
  let videos: [Components.Schemas.Video]
  let model: ChildAppModel

  var body: some View {
    LazyVGrid(columns: [GridItem(.adaptive(minimum: 250, maximum: 420), spacing: 20)], spacing: 24)
    {
      ForEach(videos, id: \.id) { video in
        NavigationLink {
          WatchPageView(videoID: video.id, model: model)
        } label: {
          VideoCard(video: video)
        }
        .buttonStyle(.plain)
      }
    }
  }
}
