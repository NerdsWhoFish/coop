import CoopKit
import SwiftUI

struct ChildSearchView: View {
  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  @Bindable var model: ChildAppModel
  @State private var query = ""
  @State private var results: Components.Schemas.SearchResults?
  @State private var askedChannels: Set<String> = []
  @State private var isSearching = false

  var body: some View {
    NavigationStack {
      ScrollView {
        if let results, !results.channels.isEmpty || !results.videos.isEmpty {
          VStack(alignment: .leading, spacing: 26) {
            if !results.channels.isEmpty {
              Text("CHANNELS").font(.caption.weight(.black)).tracking(1.6).foregroundStyle(
                WatchTheme.purple)
              ForEach(results.channels, id: \.value1.id) { result in
                NavigationLink {
                  ChannelPageView(channelID: result.value1.id, model: model)
                } label: {
                  HStack(spacing: 13) {
                    AsyncImage(url: result.value1.thumbnailUrl.flatMap(URL.init(string:))) {
                      image in
                      image.resizable().scaledToFill()
                    } placeholder: {
                      Image(systemName: "play.rectangle.fill")
                    }
                    .frame(width: 58, height: 58).clipShape(.circle)
                    Text(result.value1.title).font(.title3.bold()).foregroundStyle(
                      WatchTheme.foreground)
                    Spacer()
                    if result.value2.state == .requestable {
                      Image(
                        systemName: result.value2.pendingRequest ?? false
                          ? "checkmark.seal.fill" : "lock.fill"
                      )
                      .foregroundStyle(
                        result.value2.pendingRequest ?? false
                          ? WatchTheme.green : WatchTheme.yellow)
                    }
                  }
                }
                .buttonStyle(.plain)
              }
            }

            if !results.videos.isEmpty {
              Text("VIDEOS").font(.caption.weight(.black)).tracking(1.6).foregroundStyle(
                WatchTheme.cyan)
              LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 250, maximum: 420), spacing: 20)],
                spacing: 24
              ) {
                ForEach(results.videos, id: \.id) { video in
                  if video.locked ?? false {
                    Button {
                      ask(video)
                    } label: {
                      VideoCard(video: video, locked: !askedChannels.contains(video.channelId))
                        .overlay(alignment: .topTrailing) {
                          if askedChannels.contains(video.channelId) {
                            Text("ASKED").font(.caption.weight(.black)).tracking(1.2)
                              .padding(8).background(WatchTheme.green, in: .capsule)
                              .foregroundStyle(WatchTheme.background)
                              .padding(8)
                          }
                        }
                    }
                    .buttonStyle(.plain).disabled(askedChannels.contains(video.channelId))
                  } else {
                    VideoCard(video: video, model: model)
                  }
                }
              }
            }
          }
          .padding()
        } else {
          ContentUnavailableView.search(text: query)
            .containerRelativeFrame(.vertical, alignment: .center)
        }
      }
      .refreshable { await performSearch() }
      .searchable(text: $query, prompt: "Channels and videos")
      .onSubmit(of: .search, search)
      .navigationTitle("Find something")
      .toolbar { if isSearching { ProgressView() } }
      .watchBackground()
    }
  }

  private func search() {
    Task { await performSearch() }
  }

  private func performSearch() async {
    let term = query.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !term.isEmpty else { return }
    isSearching = true
    defer { isSearching = false }
    do { results = try await model.search(query: term) } catch {
      model.errorMessage = error.localizedDescription
    }
  }

  private func ask(_ video: Components.Schemas.Video) {
    Task {
      do {
        try await model.requestChannel(channelID: video.channelId, videoID: video.id)
        _ = withAnimation(reduceMotion ? nil : .spring(duration: 0.3, bounce: 0.35)) {
          askedChannels.insert(video.channelId)
        }
      } catch { model.errorMessage = error.localizedDescription }
    }
  }
}
