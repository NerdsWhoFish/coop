import SwiftUI

struct FamilyRecommendationTuningView: View {
  @Bindable var model: AppModel

  @State private var channels: [TunableChannel] = []
  @State private var weights: [String: Int] = [:]
  @State private var savingChannels: Set<String> = []
  @State private var isLoading = true
  @Environment(\.dynamicTypeSize) private var dynamicTypeSize

  var body: some View {
    ScrollView {
      LazyVStack(alignment: .leading, spacing: 28) {
        introduction
        PreferenceMixStrip(channels: channels, weights: weights)
        channelMixer
      }
      .padding(.horizontal)
      .padding(.vertical, 20)
      .frame(maxWidth: 920)
      .frame(maxWidth: .infinity)
    }
    .background(CoopTheme.background)
    .navigationTitle("All children mix")
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
    Image(systemName: "person.3.sequence.fill")
      .font(.system(size: 30, weight: .bold))
      .foregroundStyle(CoopTheme.background)
      .frame(width: 56, height: 56)
      .background(CoopTheme.cyan, in: .rect(cornerRadius: 16))
      .accessibilityHidden(true)
  }

  private var introductionCopy: some View {
    VStack(alignment: .leading, spacing: 6) {
      Text("ONE MIX, EVERY CHILD")
        .font(.caption.weight(.black))
        .tracking(1.8)
        .foregroundStyle(CoopTheme.cyan)
      Text("Shape every child’s feed")
        .font(.title2.weight(.black))
      Text(
        "These defaults apply to every child, including new profiles. "
          + "Changing a channel here replaces individual adjustments for that channel."
      )
      .font(.subheadline)
      .foregroundStyle(CoopTheme.foreground.opacity(0.72))
      .fixedSize(horizontal: false, vertical: true)
    }
  }

  @ViewBuilder
  private var channelMixer: some View {
    VStack(alignment: .leading, spacing: 12) {
      Text("HOUSEHOLD CHANNEL MIXER")
        .font(.caption.weight(.black))
        .tracking(1.8)
        .foregroundStyle(CoopTheme.purple)
      Text("Adjust everyone together")
        .font(.title3.weight(.bold))
      Text("A lower setting gives other approved channels more room. It never blocks a channel.")
        .font(.subheadline)
        .foregroundStyle(CoopTheme.foreground.opacity(0.7))

      if isLoading {
        HStack {
          Spacer()
          ProgressView()
          Spacer()
        }
        .frame(minHeight: 120)
      } else if channels.isEmpty {
        ContentUnavailableView(
          "No family-wide channels",
          systemImage: "slider.horizontal.3",
          description: Text("Approve a channel for everyone, then tune it here.")
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
      async let channelLoad = model.familyTunableChannels()
      async let weightLoad = model.familyRecommendationChannelWeights()
      (channels, weights) = try await (channelLoad, weightLoad)
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
        try await model.setFamilyRecommendationChannelWeight(next, channelID: channel.id)
      } catch {
        weights[channel.id] = previous
        model.errorMessage = error.localizedDescription
      }
      savingChannels.remove(channel.id)
    }
  }
}
