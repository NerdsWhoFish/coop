import CoopKit
import SwiftUI

struct RulesView: View {
  @Bindable var model: AppModel
  @State private var childID: String?
  @State private var globalChannels: [Components.Schemas.ApprovedChannel] = []
  @State private var childChannels: [Components.Schemas.EffectiveChannel] = []
  @State private var blockedChannels: [Components.Schemas.BlockedChannel] = []
  @State private var keywords: [Components.Schemas.Keyword] = []
  @State private var isLoading = false
  @State private var showingSearch = false
  @State private var showingKeyword = false

  var body: some View {
    NavigationStack {
      List {
        Section {
          Picker("Rule scope", selection: $childID) {
            Text("Everyone").tag(String?.none)
            ForEach(model.children, id: \.value1.id) { child in
              Text(child.value1.name).tag(Optional(child.value1.id))
            }
          }
          .pickerStyle(.menu)
        } footer: {
          Text(
            childID == nil
              ? "Family rules affect every child."
              : "Child rules add to or subtract from the family baseline.")
        }

        Section("Approved channels") {
          if approvedChannelsAreEmpty {
            Text("No channels cleared for this scope.")
              .foregroundStyle(CoopTheme.foreground.opacity(0.65))
          } else if childID == nil {
            ForEach(globalChannels, id: \.value1.id) { channel in
              ChannelPolicyRow(channel: channel.value1, badge: "EVERYONE", denied: false)
                .swipeActions {
                  Button("Remove", role: .destructive) { remove(channel.value1.id) }
                }
            }
          } else {
            ForEach(childChannels, id: \.value1.value1.id) { channel in
              ChildChannelPolicyRow(channel: channel) { action in
                change(channel: channel, action: action)
              }
            }
          }
        }

        Section("Blocked family-wide") {
          if blockedChannels.isEmpty {
            Text("No channels are hidden family-wide.")
              .foregroundStyle(CoopTheme.foreground.opacity(0.65))
          } else {
            ForEach(blockedChannels, id: \.value1.id) { channel in
              ChannelPolicyRow(channel: channel.value1, badge: "BLOCKED", denied: true)
                .swipeActions {
                  Button("Unblock") { unblock(channel.value1.id) }
                    .tint(CoopTheme.green)
                }
            }
          }
        }

        Section("Hidden words") {
          if keywords.isEmpty {
            Text("No negative keywords for this scope.")
              .foregroundStyle(CoopTheme.foreground.opacity(0.65))
          } else {
            ForEach(keywords, id: \.value1.id) { keyword in
              VStack(alignment: .leading, spacing: 3) {
                Text(keyword.value2.term)
                  .font(.headline)
                Text(keywordSummary(keyword.value2))
                  .font(.caption)
                  .foregroundStyle(CoopTheme.foreground.opacity(0.62))
              }
              .swipeActions {
                Button("Delete", role: .destructive) { delete(keyword.value1.id) }
              }
            }
          }
        }
      }
      .scrollContentBackground(.hidden)
      .background(CoopTheme.background)
      .navigationTitle("Access rules")
      .toolbar {
        ToolbarItemGroup(placement: .topBarTrailing) {
          if isLoading { ProgressView() }
          Button("Add keyword", systemImage: "text.badge.plus") { showingKeyword = true }
          Button("Find channel", systemImage: "magnifyingglass") { showingSearch = true }
        }
      }
      .refreshable { await load() }
      .task(id: childID) { await load() }
      .sheet(isPresented: $showingSearch) {
        ChannelSearchView(model: model, childID: childID) { await load() }
      }
      .sheet(isPresented: $showingKeyword) {
        AddKeywordView(model: model, childID: childID) { await load() }
      }
      .coopBackground()
    }
  }

  private var approvedChannelsAreEmpty: Bool {
    childID == nil ? globalChannels.isEmpty : childChannels.isEmpty
  }

  private func load() async {
    isLoading = true
    defer { isLoading = false }
    do {
      async let keywordLoad = model.keywords(childID: childID)
      async let blockLoad = model.blocklist()
      if let childID {
        childChannels = try await model.childAllowlist(childID: childID)
      } else {
        globalChannels = try await model.globalAllowlist()
      }
      (keywords, blockedChannels) = try await (keywordLoad, blockLoad)
    } catch {
      model.errorMessage = error.localizedDescription
    }
  }

  private func remove(_ channelID: String) {
    mutate { try await model.removeChannel(channelID, childID: childID) }
  }

  private func unblock(_ channelID: String) {
    mutate { try await model.setChannelBlocked(false, channelID: channelID) }
  }

  private func delete(_ keywordID: String) {
    mutate { try await model.deleteKeyword(id: keywordID) }
  }

  private func change(channel: Components.Schemas.EffectiveChannel, action: ChildChannelAction) {
    guard let childID else { return }
    let channelID = channel.value1.value1.id
    switch action {
    case .remove:
      mutate { try await model.removeChannel(channelID, childID: childID) }
    case .deny:
      mutate { try await model.setChannelDenied(true, channelID: channelID, childID: childID) }
    case .restore:
      mutate { try await model.setChannelDenied(false, channelID: channelID, childID: childID) }
    }
  }

  private func mutate(_ operation: @escaping () async throws -> Void) {
    Task {
      do {
        try await operation()
        await load()
      } catch {
        model.errorMessage = error.localizedDescription
      }
    }
  }

  private func keywordSummary(_ keyword: Components.Schemas.KeywordInput) -> String {
    var fields: [String] = []
    if keyword.matchTitle ?? true { fields.append("titles") }
    if keyword.matchTags ?? true { fields.append("tags") }
    if keyword.matchDescription ?? false { fields.append("descriptions") }
    return fields.joined(separator: ", ") + ((keyword.wholeWord ?? false) ? " · whole word" : "")
  }
}

private enum ChildChannelAction {
  case remove
  case deny
  case restore
}

private struct ChildChannelPolicyRow: View {
  let channel: Components.Schemas.EffectiveChannel
  let action: (ChildChannelAction) -> Void

  var body: some View {
    ChannelPolicyRow(
      channel: channel.value1.value1,
      badge: channel.value2.source == .global ? "FAMILY" : "JUST THIS CHILD",
      denied: channel.value2.deniedForChild ?? false
    )
    .swipeActions {
      if channel.value2.deniedForChild ?? false {
        Button("Restore") { action(.restore) }.tint(CoopTheme.green)
      } else if channel.value2.source == .global {
        Button("Hide") { action(.deny) }.tint(CoopTheme.orange)
      } else {
        Button("Remove", role: .destructive) { action(.remove) }
      }
    }
  }
}

private struct ChannelPolicyRow: View {
  let channel: Components.Schemas.Channel
  let badge: String
  let denied: Bool
  @Environment(\.openURL) private var openURL

  var body: some View {
    Button {
      if let url = URL(string: "https://youtube.com/channel/\(channel.id)") { openURL(url) }
    } label: {
      HStack(spacing: 12) {
        AsyncImage(url: channel.thumbnailUrl.flatMap(URL.init(string:))) { image in
          image.resizable().scaledToFill()
        } placeholder: {
          Image(systemName: "play.rectangle.fill").foregroundStyle(CoopTheme.muted)
        }
        .frame(width: 44, height: 44)
        .clipShape(.circle)

        VStack(alignment: .leading, spacing: 4) {
          Text(channel.title).font(.headline).foregroundStyle(CoopTheme.foreground)
          Text(denied ? "HIDDEN" : badge)
            .font(.caption2.weight(.black)).tracking(1.1)
            .foregroundStyle(denied ? CoopTheme.red : CoopTheme.cyan)
        }
        Spacer()
        Image(systemName: "arrow.up.right.square").foregroundStyle(CoopTheme.muted)
      }
    }
    .buttonStyle(.plain)
  }
}
