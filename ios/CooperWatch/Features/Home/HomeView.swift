import CoopKit
import SwiftUI
import UIKit

struct HomeView: View {
  @Bindable var model: ChildAppModel
  @State private var phoneSection = PhoneSection.recommendations

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
          if UIDevice.current.userInterfaceIdiom == .pad {
            padSections
          } else {
            phoneSections
          }
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

  private var subscribedVideos: [Components.Schemas.Video] {
    let channelIDs = Set(model.subscriptions.map(\.id))
    return model.feed.filter { channelIDs.contains($0.channelId) }
  }

  private var padSections: some View {
    VStack(alignment: .leading, spacing: 34) {
      HomeVideoShelf(
        title: "Recommendations",
        systemImage: "sparkles.tv.fill",
        videos: model.feed,
        model: model,
        emptyMessage: "Recommendations will appear as you watch."
      )
      .accessibilityIdentifier("home-recommendations-section")

      HomeVideoShelf(
        title: "Subscriptions",
        systemImage: "play.square.stack.fill",
        videos: subscribedVideos,
        model: model,
        emptyMessage: "Subscribe to a channel to keep its videos here."
      )
      .accessibilityIdentifier("home-subscriptions-section")

      DiscoveryShelf(
        title: "Discover",
        discoveries: model.discoveries,
        model: model,
        horizontal: true
      )
      .accessibilityIdentifier("home-discover-section")
    }
    .padding()
  }

  private var phoneSections: some View {
    VStack(alignment: .leading, spacing: 20) {
      HStack(spacing: 4) {
        ForEach(PhoneSection.allCases) { section in
          Button {
            phoneSection = section
          } label: {
            Text(section.title)
              .font(.caption.weight(.bold))
              .lineLimit(1)
              .minimumScaleFactor(0.8)
              .frame(maxWidth: .infinity)
              .padding(.vertical, 9)
              .background(
                phoneSection == section ? WatchTheme.muted : .clear,
                in: .capsule
              )
          }
          .buttonStyle(.plain)
          .accessibilityAddTraits(phoneSection == section ? .isSelected : [])
        }
      }
      .padding(4)
      .background(WatchTheme.surface, in: .capsule)
      .accessibilityIdentifier("home-section-picker")

      switch phoneSection {
      case .recommendations:
        phoneVideoSection(
          videos: model.feed,
          emptyTitle: "Recommendations are warming up",
          emptyMessage: "Watch a few videos and Coop will learn what you enjoy."
        )
      case .subscriptions:
        phoneVideoSection(
          videos: subscribedVideos,
          emptyTitle: "No subscriptions yet",
          emptyMessage: "Open a channel and tap Subscribe to keep it here."
        )
      case .discover:
        if model.discoveries.isEmpty {
          emptySection(
            title: "Nothing new right now",
            systemImage: "sparkles",
            message: "New channel ideas will show up here."
          )
        } else {
          DiscoveryShelf(title: "Discover", discoveries: model.discoveries, model: model)
        }
      }
    }
    .padding()
  }

  @ViewBuilder
  private func phoneVideoSection(
    videos: [Components.Schemas.Video], emptyTitle: String, emptyMessage: String
  ) -> some View {
    if videos.isEmpty {
      emptySection(title: emptyTitle, systemImage: "play.rectangle", message: emptyMessage)
    } else {
      VideoGrid(videos: videos, model: model)
    }
  }

  private func emptySection(title: String, systemImage: String, message: String) -> some View {
    ContentUnavailableView(title, systemImage: systemImage, description: Text(message))
      .frame(maxWidth: .infinity)
      .padding(.vertical, 50)
  }
}

private enum PhoneSection: String, CaseIterable, Identifiable {
  case recommendations
  case subscriptions
  case discover

  var id: Self { self }
  var title: String { rawValue.capitalized }
}

private struct HomeVideoShelf: View {
  let title: String
  let systemImage: String
  let videos: [Components.Schemas.Video]
  let model: ChildAppModel
  let emptyMessage: String

  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      Label(title, systemImage: systemImage)
        .font(.title2.bold())
        .foregroundStyle(WatchTheme.cyan)

      if videos.isEmpty {
        Text(emptyMessage)
          .foregroundStyle(WatchTheme.foreground.opacity(0.7))
          .padding(.vertical, 30)
      } else {
        ScrollView(.horizontal) {
          LazyHStack(alignment: .top, spacing: 18) {
            ForEach(videos, id: \.id) { video in
              VideoCard(video: video, model: model)
                .frame(width: 320)
            }
          }
          .scrollTargetLayout()
        }
        .scrollIndicators(.hidden)
        .scrollTargetBehavior(.viewAligned)
      }
    }
  }
}
