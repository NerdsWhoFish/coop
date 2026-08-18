import CoopKit
import SwiftUI

struct ChannelSearchView: View {
  @Bindable var model: AppModel
  let childID: String?
  let didChange: () async -> Void
  @Environment(\.dismiss) private var dismiss
  @State private var query = ""
  @State private var results: [Components.Schemas.Channel] = []
  @State private var cleared: Set<String> = []
  @State private var blocked: Set<String> = []
  @State private var isSearching = false

  var body: some View {
    NavigationStack {
      Group {
        if results.isEmpty {
          ContentUnavailableView.search(text: query)
        } else {
          List(results, id: \.id) { channel in
            HStack {
              VStack(alignment: .leading, spacing: 4) {
                Text(channel.title).font(.headline)
                if let subscribers = channel.subscriberCount {
                  Text("\(subscribers.formatted()) subscribers")
                    .font(.caption).foregroundStyle(CoopTheme.foreground.opacity(0.62))
                }
              }
              Spacer()
              Menu {
                Button(childID == nil ? "Approve for everyone" : "Approve for this child") {
                  approve(channel.id)
                }
                Button("Block family-wide", role: .destructive) { block(channel.id) }
              } label: {
                Label(resultLabel(channel.id), systemImage: resultSymbol(channel.id))
              }
              .buttonStyle(.borderedProminent)
              .tint(
                blocked.contains(channel.id)
                  ? CoopTheme.red
                  : (cleared.contains(channel.id) ? CoopTheme.green : CoopTheme.cyan)
              )
              .foregroundStyle(CoopTheme.background)
              .disabled(cleared.contains(channel.id) || blocked.contains(channel.id))
            }
          }
          .scrollContentBackground(.hidden)
        }
      }
      .searchable(text: $query, prompt: "YouTube channel")
      .onSubmit(of: .search, search)
      .navigationTitle(childID == nil ? "Approve or block channels" : "Approve for this child")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar {
        ToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() } }
        if isSearching { ToolbarItem(placement: .topBarTrailing) { ProgressView() } }
      }
      .coopBackground()
    }
  }

  private func search() {
    let normalized = query.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !normalized.isEmpty else { return }
    isSearching = true
    Task {
      do {
        results = try await model.searchChannels(query: normalized)
      } catch {
        model.errorMessage = error.localizedDescription
      }
      isSearching = false
    }
  }

  private func approve(_ channelID: String) {
    Task {
      do {
        try await model.allowChannel(channelID, childID: childID)
        cleared.insert(channelID)
        await didChange()
      } catch {
        model.errorMessage = error.localizedDescription
      }
    }
  }

  private func block(_ channelID: String) {
    Task {
      do {
        try await model.setChannelBlocked(true, channelID: channelID)
        blocked.insert(channelID)
        await didChange()
      } catch {
        model.errorMessage = error.localizedDescription
      }
    }
  }

  private func resultLabel(_ channelID: String) -> String {
    if blocked.contains(channelID) { return "Blocked" }
    if cleared.contains(channelID) { return "Approved" }
    return "Decide"
  }

  private func resultSymbol(_ channelID: String) -> String {
    if blocked.contains(channelID) { return "nosign" }
    if cleared.contains(channelID) { return "checkmark" }
    return "ellipsis"
  }
}
