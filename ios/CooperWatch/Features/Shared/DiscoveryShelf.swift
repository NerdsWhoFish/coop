import CoopKit
import SwiftUI

struct DiscoveryShelf: View {
  let title: String
  let discoveries: [Components.Schemas.Discovery]
  let model: ChildAppModel
  var horizontal = false

  var body: some View {
    if !discoveries.isEmpty {
      VStack(alignment: .leading, spacing: 16) {
        HStack(alignment: .firstTextBaseline) {
          Label(title, systemImage: "sparkles.rectangle.stack.fill")
            .font(.title2.bold())
            .foregroundStyle(WatchTheme.yellow)
          Spacer()
          Text("NEW CHANNELS")
            .font(.caption2.weight(.black))
            .tracking(1.2)
            .foregroundStyle(WatchTheme.background)
            .padding(.horizontal, 9)
            .padding(.vertical, 6)
            .background(WatchTheme.yellow, in: .capsule)
        }

        Text("These match what you enjoy, but a grown-up still has to unlock the channel.")
          .font(.subheadline)
          .foregroundStyle(WatchTheme.foreground.opacity(0.7))

        if horizontal {
          ScrollView(.horizontal) {
            LazyHStack(alignment: .top, spacing: 18) {
              discoveryCards
            }
            .scrollTargetLayout()
          }
          .scrollIndicators(.hidden)
          .scrollTargetBehavior(.viewAligned)
        } else {
          LazyVStack(spacing: 22) {
            discoveryCards
          }
        }
      }
      .accessibilityIdentifier("discovery-shelf")
    }
  }

  @ViewBuilder
  private var discoveryCards: some View {
    ForEach(discoveries, id: \.video.id) { discovery in
      NavigationLink {
        ChannelPageView(
          channelID: discovery.video.channelId,
          promptedByVideoID: discovery.video.id,
          model: model
        )
      } label: {
        VStack(alignment: .leading, spacing: 10) {
          VideoCard(video: discovery.video, locked: true)
          Label(discovery.reason, systemImage: "wand.and.stars")
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(WatchTheme.yellow)
            .lineLimit(2)
          Label(
            discovery.pendingRequest ? "Waiting for a grown-up" : "Preview channel",
            systemImage: discovery.pendingRequest
              ? "clock.badge.checkmark" : "arrow.right.circle.fill"
          )
          .font(.caption.weight(.black))
          .textCase(.uppercase)
          .tracking(0.8)
          .foregroundStyle(
            discovery.pendingRequest ? WatchTheme.green : WatchTheme.foreground.opacity(0.72)
          )
        }
        .frame(width: horizontal ? 320 : nil)
        .padding(14)
        .background(WatchTheme.yellow.opacity(0.08), in: .rect(cornerRadius: 18))
        .overlay {
          RoundedRectangle(cornerRadius: 18)
            .stroke(WatchTheme.yellow.opacity(0.34), lineWidth: 1)
        }
      }
      .buttonStyle(.plain)
      .accessibilityIdentifier("discovery-\(discovery.video.id)")
    }
  }
}
