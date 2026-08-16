import CoopKit
import SwiftUI

struct ShortOccurrence: Identifiable {
  let id = UUID()
  let video: Components.Schemas.Video
}

struct ShortsFeedView: View {
  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  @Environment(\.scenePhase) private var scenePhase
  @Bindable var model: ChildAppModel
  @State private var sessionID = UUID().uuidString
  @State private var items: [ShortOccurrence] = []
  @State private var currentID: ShortOccurrence.ID?
  @State private var nextOffset = 0
  @State private var isLoading = false
  @State private var loadError: String?
  @State private var isVisible = false

  private let pageSize = 8

  var body: some View {
    NavigationStack {
      Group {
        if items.isEmpty, isLoading {
          ProgressView("Mixing your Shorts…")
            .controlSize(.large)
        } else if items.isEmpty {
          ScrollView {
            emptyState
              .containerRelativeFrame(.vertical, alignment: .center)
          }
          .refreshable { await reshuffle() }
        } else {
          feed
        }
      }
      .navigationTitle("Shorts")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar {
        Button {
          Task { await reshuffle() }
        } label: {
          Image(systemName: "shuffle")
        }
        .disabled(isLoading)
        .accessibilityLabel("Shuffle Shorts")
      }
      .watchBackground()
    }
    .task { await loadNextPage() }
    .onAppear { isVisible = true }
    .onDisappear { isVisible = false }
  }

  private var feed: some View {
    ScrollView(.vertical) {
      LazyVStack(spacing: 0) {
        ForEach(Array(items.enumerated()), id: \.element.id) { index, item in
          ShortPageView(
            video: item.video,
            position: index + 1,
            isFirst: index == 0,
            isActive: isVisible && scenePhase == .active && currentID == item.id,
            model: model
          )
          .containerRelativeFrame(.vertical)
          .id(item.id)
        }
      }
      .scrollTargetLayout()
    }
    .scrollIndicators(.hidden)
    .scrollPosition(id: $currentID)
    .scrollTargetBehavior(.paging)
    .refreshable { await reshuffle() }
    .sensoryFeedback(.selection, trigger: currentID)
    .overlay(alignment: .top) {
      if let loadError {
        HStack(spacing: 10) {
          Image(systemName: "wifi.exclamationmark")
          Text(loadError).lineLimit(2)
          Button("Retry") { Task { await loadNextPage() } }
            .fontWeight(.bold)
        }
        .font(.footnote)
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(.ultraThinMaterial, in: .rect(cornerRadius: 14))
        .padding(12)
      }
    }
    .onChange(of: currentID) { _, newValue in
      guard let newValue else { return }
      Task { await loadMoreIfNeeded(currentID: newValue) }
    }
  }

  private var emptyState: some View {
    ContentUnavailableView {
      Label("No Shorts yet", systemImage: "bolt.slash.fill")
    } description: {
      Text(loadError ?? "Approved Shorts will appear here after your channels refresh.")
    } actions: {
      Button("Try again") {
        Task { await reshuffle() }
      }
      .buttonStyle(.borderedProminent)
      .tint(WatchTheme.pink)
    }
  }

  private func loadMoreIfNeeded(currentID: ShortOccurrence.ID) async {
    guard let index = items.firstIndex(where: { $0.id == currentID }) else { return }
    guard index >= items.count - 3 else { return }
    await loadNextPage()
  }

  private func reshuffle() async {
    let animation: Animation? = reduceMotion ? nil : .spring(duration: 0.35, bounce: 0.18)
    withAnimation(animation) {
      items = []
      currentID = nil
    }
    sessionID = UUID().uuidString
    nextOffset = 0
    loadError = nil
    await loadNextPage()
  }

  private func loadNextPage() async {
    guard !isLoading else { return }
    isLoading = true
    defer { isLoading = false }

    do {
      loadError = nil
      let videos = try await model.shorts(
        session: sessionID,
        offset: nextOffset,
        limit: pageSize
      )
      guard !Task.isCancelled else { return }
      let newItems = videos.map(ShortOccurrence.init(video:))
      items.append(contentsOf: newItems)
      nextOffset += videos.count
      loadError = nil
      if currentID == nil {
        currentID = newItems.first?.id
      }
    } catch {
      loadError = error.localizedDescription
    }
  }
}
