import CoopKit
import SwiftUI

struct RecommendationTuningView: View {
  let child: Components.Schemas.Child
  @Bindable var model: AppModel

  @State private var recommendations: [FeedRecommendation] = []
  @State private var channels: [TunableChannel] = []
  @State private var weights: [String: Int] = [:]
  @State private var savingChannels: Set<String> = []
  @State private var isLoading = true
  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  @Environment(\.dynamicTypeSize) private var dynamicTypeSize

  var body: some View {
    ScrollView {
      LazyVStack(alignment: .leading, spacing: 28) {
        introduction
        mixStrip
        recommendationPreview
        channelMixer
      }
      .padding(.horizontal)
      .padding(.vertical, 20)
      .frame(maxWidth: 920)
      .frame(maxWidth: .infinity)
    }
    .background(CoopTheme.background)
    .navigationTitle("Recommendation mix")
    .navigationBarTitleDisplayMode(.inline)
    .refreshable { await load() }
    .task { await load() }
    .coopBackground()
  }

  private var introduction: some View {
    Group {
      if dynamicTypeSize.isAccessibilitySize {
        VStack(alignment: .leading, spacing: 16) {
          introductionIcon
          introductionCopy
        }
      } else {
        HStack(alignment: .top, spacing: 16) {
          introductionIcon
          introductionCopy
        }
      }
    }
  }

  private var introductionIcon: some View {
    Image(systemName: "slider.horizontal.below.square.and.square.filled")
      .font(.system(size: 30, weight: .bold))
      .foregroundStyle(CoopTheme.background)
      .frame(width: 56, height: 56)
      .background(CoopTheme.yellow, in: .rect(cornerRadius: 16))
      .accessibilityHidden(true)
  }

  private var introductionCopy: some View {
    VStack(alignment: .leading, spacing: 6) {
      Text("TUNE, DON’T BAN")
        .font(.caption.weight(.black))
        .tracking(1.8)
        .foregroundStyle(CoopTheme.yellow)
      Text("Shape \(child.value1.name)’s feed")
        .font(.title2.weight(.black))
      Text(
        "Coop still respects every access rule. These controls only change the mix inside the approved catalog."
      )
      .font(.subheadline)
      .foregroundStyle(CoopTheme.foreground.opacity(0.72))
      .fixedSize(horizontal: false, vertical: true)
    }
  }

  private var mixStrip: some View {
    PreferenceMixStrip(channels: channels, weights: weights)
  }

  @ViewBuilder
  private var recommendationPreview: some View {
    VStack(alignment: .leading, spacing: 12) {
      Text("WHY THESE VIDEOS")
        .font(.caption.weight(.black))
        .tracking(1.8)
        .foregroundStyle(CoopTheme.cyan)
      Text("The top of the feed, explained")
        .font(.title3.weight(.bold))

      if isLoading {
        HStack {
          Spacer()
          ProgressView()
          Spacer()
        }
        .frame(minHeight: 170)
      } else if recommendations.isEmpty {
        ContentUnavailableView(
          "Nothing to rank yet",
          systemImage: "rectangle.stack.badge.questionmark",
          description: Text("Approve channels and refresh their uploads to build a preview.")
        )
      } else {
        ScrollView(.horizontal) {
          LazyHStack(alignment: .top, spacing: 14) {
            ForEach(Array(recommendations.prefix(8).enumerated()), id: \.element.id) { index, item in
              RecommendationCard(position: index + 1, recommendation: item)
                .containerRelativeFrame(.horizontal) { length, _ in min(length * 0.86, 360) }
            }
          }
          .scrollTargetLayout()
        }
        .contentMargins(.horizontal, 1, for: .scrollContent)
        .scrollTargetBehavior(.viewAligned)
        .scrollIndicators(.hidden)
        .animation(reduceMotion ? nil : .snappy, value: recommendations)
      }
    }
  }

  @ViewBuilder
  private var channelMixer: some View {
    VStack(alignment: .leading, spacing: 12) {
      Text("CHANNEL MIXER")
        .font(.caption.weight(.black))
        .tracking(1.8)
        .foregroundStyle(CoopTheme.purple)
      Text("Adjust the balance")
        .font(.title3.weight(.bold))
      Text(
        "A lower setting never hides a channel. It just gives other approved channels more room."
      )
      .font(.subheadline)
      .foregroundStyle(CoopTheme.foreground.opacity(0.7))

      if !isLoading && channels.isEmpty {
        ContentUnavailableView(
          "No approved channels",
          systemImage: "slider.horizontal.3",
          description: Text("Approve a channel for \(child.value1.name), then tune it here.")
        )
      } else {
        LazyVStack(spacing: 10) {
          ForEach(channels) { channel in
            ChannelMixerRow(
              channel: channel,
              preference: ChannelPreference(rawValue: weights[channel.id] ?? 0) ?? .balanced,
              isSaving: savingChannels.contains(channel.id),
              decrease: { change(channel, by: -1) },
              increase: { change(channel, by: 1) }
            )
          }
        }
      }
    }
  }

  private func load() async {
    isLoading = true
    defer { isLoading = false }
    do {
      async let recommendationLoad = model.feedRecommendations(childID: child.value1.id)
      async let channelLoad = model.tunableChannels(childID: child.value1.id)
      async let weightLoad = model.recommendationChannelWeights(childID: child.value1.id)
      (recommendations, channels, weights) = try await (
        recommendationLoad, channelLoad, weightLoad
      )
    } catch {
      model.errorMessage = error.localizedDescription
    }
  }

  private func change(_ channel: TunableChannel, by delta: Int) {
    let previous = weights[channel.id] ?? 0
    let next = min(max(previous + delta, -2), 2)
    guard next != previous else { return }

    weights[channel.id] = next
    savingChannels.insert(channel.id)
    Task {
      do {
        try await model.setRecommendationChannelWeight(
          next,
          channelID: channel.id,
          childID: child.value1.id
        )
        recommendations = try await model.feedRecommendations(childID: child.value1.id)
      } catch {
        weights[channel.id] = previous
        model.errorMessage = error.localizedDescription
      }
      savingChannels.remove(channel.id)
    }
  }
}

private struct RecommendationCard: View {
  let position: Int
  let recommendation: FeedRecommendation

  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      ZStack(alignment: .topLeading) {
        AsyncImage(url: recommendation.thumbnailURL) { image in
          image.resizable().scaledToFill()
        } placeholder: {
          LinearGradient(
            colors: [CoopTheme.purple.opacity(0.7), CoopTheme.cyan.opacity(0.35)],
            startPoint: .topLeading,
            endPoint: .bottomTrailing
          )
          .overlay {
            Image(systemName: signalIcon)
              .font(.system(size: 34, weight: .bold))
              .foregroundStyle(CoopTheme.foreground.opacity(0.8))
          }
        }
        .frame(maxWidth: .infinity)
        .aspectRatio(16 / 9, contentMode: .fit)
        .clipShape(.rect(cornerRadius: 14))

        Text("#\(position)")
          .font(.caption.weight(.black))
          .padding(.horizontal, 9)
          .padding(.vertical, 6)
          .foregroundStyle(CoopTheme.background)
          .background(CoopTheme.yellow, in: .capsule)
          .padding(10)
      }

      Text(recommendation.channelTitle.uppercased())
        .font(.caption2.weight(.black))
        .tracking(1)
        .foregroundStyle(CoopTheme.cyan)
      Text(recommendation.title)
        .font(.headline)
        .lineLimit(2)
      Label(recommendation.reason, systemImage: signalIcon)
        .font(.caption)
        .foregroundStyle(CoopTheme.foreground.opacity(0.72))
        .fixedSize(horizontal: false, vertical: true)
    }
    .padding(14)
    .background(CoopTheme.surface, in: .rect(cornerRadius: 20))
    .accessibilityElement(children: .combine)
    .accessibilityLabel(
      "Number \(position), \(recommendation.title), \(recommendation.reason)"
    )
  }

  private var signalIcon: String {
    switch recommendation.signal {
    case .liked: "hand.thumbsup.fill"
    case .disliked: "hand.thumbsdown.fill"
    case .parentMore: "arrow.up.right"
    case .parentLess: "arrow.down.right"
    case .rewatched: "arrow.counterclockwise"
    case .completed: "checkmark.circle.fill"
    case .channelSatisfaction: "sparkles"
    case .newSubscription: "bell.fill"
    case .unwatched: "binoculars.fill"
    case .recent: "clock.fill"
    }
  }
}

struct PreferenceMixStrip: View {
  let channels: [TunableChannel]
  let weights: [String: Int]
  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  @Environment(\.dynamicTypeSize) private var dynamicTypeSize

  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      Group {
        if dynamicTypeSize.isAccessibilitySize {
          VStack(alignment: .leading, spacing: 8) {
            title
            channelCount
          }
        } else {
          HStack {
            title
            Spacer()
            channelCount
          }
        }
      }

      if channels.isEmpty {
        Capsule().fill(CoopTheme.surface).frame(height: 14)
      } else {
        HStack(spacing: 4) {
          ForEach(channels) { channel in
            Capsule()
              .fill(preferenceColor(weights[channel.id] ?? 0))
              .frame(height: 14)
              .accessibilityHidden(true)
          }
        }
        .animation(reduceMotion ? nil : .snappy, value: weights)
      }

      Group {
        if dynamicTypeSize.isAccessibilitySize {
          VStack(alignment: .leading) {
            Label("Less", systemImage: "minus")
            Label("Balanced", systemImage: "equal")
            Label("More", systemImage: "plus")
          }
        } else {
          HStack {
            Label("Less", systemImage: "minus")
            Spacer()
            Label("Balanced", systemImage: "equal")
            Spacer()
            Label("More", systemImage: "plus")
          }
        }
      }
      .font(.caption.weight(.semibold))
      .foregroundStyle(CoopTheme.foreground.opacity(0.62))
    }
    .padding(18)
    .background(CoopTheme.surface.opacity(0.72), in: .rect(cornerRadius: 22))
  }

  private var title: some View {
    Label("Live feed mix", systemImage: "waveform.path.ecg")
      .font(.headline)
  }

  private var channelCount: some View {
    Text("\(channels.count) CHANNELS")
      .font(.caption2.weight(.black))
      .tracking(1.1)
      .foregroundStyle(CoopTheme.muted)
  }

  private func preferenceColor(_ weight: Int) -> Color {
    switch weight {
    case ..<0: CoopTheme.orange
    case 1...: CoopTheme.green
    default: CoopTheme.muted
    }
  }
}

struct ChannelMixerRow: View {
  let channel: TunableChannel
  let preference: ChannelPreference
  let isSaving: Bool
  let decrease: () -> Void
  let increase: () -> Void
  @Environment(\.dynamicTypeSize) private var dynamicTypeSize

  var body: some View {
    Group {
      if dynamicTypeSize.isAccessibilitySize {
        VStack(alignment: .leading, spacing: 14) {
          channelIdentity
          controls
        }
      } else {
        HStack(spacing: 14) {
          channelIdentity
          Spacer(minLength: 8)
          controls
        }
      }
    }
    .padding(14)
    .background(CoopTheme.surface.opacity(0.72), in: .rect(cornerRadius: 18))
    .accessibilityElement(children: .contain)
  }

  private var channelIdentity: some View {
    HStack(spacing: 14) {
      AsyncImage(url: channel.thumbnailURL) { image in
        image.resizable().scaledToFill()
      } placeholder: {
        Image(systemName: "play.rectangle.fill")
          .foregroundStyle(CoopTheme.purple)
      }
      .frame(width: 46, height: 46)
      .background(CoopTheme.background.opacity(0.5), in: .circle)
      .clipShape(.circle)

      VStack(alignment: .leading, spacing: 4) {
        Text(channel.title)
          .font(.headline)
        Text(preference.label.uppercased())
          .font(.caption2.weight(.black))
          .tracking(1.1)
          .foregroundStyle(preferenceColor)
          .contentTransition(.numericText())
          .accessibilityIdentifier("channel-preference-\(channel.id)")
      }
    }
  }

  private var controls: some View {
    HStack(spacing: 8) {
      mixerButton(
        "Show less", systemImage: "minus", disabled: preference == .muchLess, action: decrease)
      mixerButton(
        "Show more", systemImage: "plus", disabled: preference == .muchMore, action: increase)
    }
  }

  private var preferenceColor: Color {
    switch preference.rawValue {
    case ..<0: CoopTheme.orange
    case 1...: CoopTheme.green
    default: CoopTheme.muted
    }
  }

  private func mixerButton(
    _ label: String,
    systemImage: String,
    disabled: Bool,
    action: @escaping () -> Void
  ) -> some View {
    Button(action: action) {
      Image(systemName: systemImage)
        .font(.headline.weight(.black))
        .frame(width: 40, height: 40)
        .background(CoopTheme.background.opacity(0.75), in: .circle)
    }
    .buttonStyle(.plain)
    .disabled(disabled || isSaving)
    .opacity(disabled ? 0.35 : 1)
    .accessibilityLabel("\(label) from \(channel.title)")
    .accessibilityHint("Current setting: \(preference.label)")
  }
}
