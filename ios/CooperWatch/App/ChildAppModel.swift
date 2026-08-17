import CoopKit
import Observation
import SwiftUI
import UIKit

@MainActor
@Observable
final class ChildAppModel {
  enum Destination: Equatable {
    case launching
    case pairing
    case watch
  }

  var destination: Destination = .launching
  var serverAddress: String
  var profile: Components.Schemas.ChildProfile?
  var feed: [Components.Schemas.Video] = []
  var discoveries: [Components.Schemas.Discovery] = []
  var subscriptions: [Components.Schemas.Channel] = []
  var isWorking = false
  var errorMessage: String?
  var requiredUpdate: AppRelease?
  private(set) var isPreviewMode = false

  private var api: CoopAPI?
  private let defaults = UserDefaults.standard
  private let tokenStore = SecureTokenStore(
    service: "fish.nerdswhofish.coop.child",
    account: "device-token"
  )
  private static let serverKey = "coop.child.server.address"

  init() {
    #if DEBUG
      if ProcessInfo.processInfo.environment["COOP_UI_SPLASH"] == "1" {
        isPreviewMode = true
        serverAddress = "https://coop.example"
        return
      }
      if ProcessInfo.processInfo.environment["COOP_UI_PREVIEW"] == "1" {
        isPreviewMode = true
        serverAddress = "https://coop.example"
        profile = Components.Schemas.ChildProfile(
          id: "preview-child",
          name: "Cooper",
          shortsEnabled: ProcessInfo.processInfo.environment["COOP_UI_SHORTS_DISABLED"] != "1",
          watchPageAutoplay: false,
          videoSearchTiles: true,
          channelDiscoveryEnabled: true,
          allowSelfUnpair: false
        )
        feed = Self.previewVideos
        discoveries = Self.previewDiscoveries
        subscriptions = Self.previewChannels
        destination = .watch
        if ProcessInfo.processInfo.environment["COOP_UI_UPDATE_REQUIRED"] == "1" {
          requiredUpdate = AppRelease(
            app: "child",
            title: "Cooper Watch",
            build: "13",
            installUrl: "itms-services://?action=download-manifest&url=https://coop.example/install/child.plist",
            installerUrl: "https://coop.example/install/"
          )
        }
        return
      }
    #endif
    serverAddress = defaults.string(forKey: Self.serverKey) ?? ""
  }

  func launch() async {
    #if DEBUG
      if isPreviewMode { return }
    #endif
    destination = .launching
    errorMessage = nil
    await checkForRequiredUpdate()
    guard requiredUpdate == nil else { return }
    await restore()
  }

  func restore() async {
    guard !serverAddress.isEmpty, let token = tokenStore.load() else {
      destination = .pairing
      return
    }
    await checkForRequiredUpdate()
    guard requiredUpdate == nil else { return }
    await perform {
      let serverURL = try ServerURL.normalize(serverAddress)
      let authenticatedAPI = CoopAPI(serverURL: serverURL, token: token)
      do {
        profile = try await authenticatedAPI.childProfile()
      } catch CoopAPIError.invalidSession {
        tokenStore.delete()
        destination = .pairing
        return
      }
      api = authenticatedAPI
      destination = .watch
      await loadLibrary()
    }
  }

  func checkForRequiredUpdate() async {
    #if DEBUG
      if isPreviewMode { return }
    #endif
    guard !serverAddress.isEmpty else {
      requiredUpdate = nil
      return
    }
    do {
      requiredUpdate = try await AppUpdate.requiredRelease(
        serverAddress: serverAddress,
        app: .child
      )
    } catch {
      // Update metadata is allowed to fail open so a network outage cannot lock a child out.
    }
  }

  func pair(code: String) async {
    await perform {
      let serverURL = try ServerURL.normalize(serverAddress)
      defaults.set(serverAddress, forKey: Self.serverKey)
      await checkForRequiredUpdate()
      guard requiredUpdate == nil else { return }
      let publicAPI = CoopAPI(serverURL: serverURL)
      let pairing = try await publicAPI.pairChildDevice(
        code: code,
        deviceName: UIDevice.current.name
      )
      try tokenStore.save(pairing.token)
      api = CoopAPI(serverURL: serverURL, token: pairing.token)
      profile = pairing.profile
      destination = .watch
      await loadLibrary()
    }
  }

  func loadLibrary() async {
    guard let api else { return }
    do {
      async let feedLoad = api.childFeed()
      async let subscriptionLoad = api.childSubscriptions()
      async let discoveryLoad = api.childDiscovery()
      async let profileLoad = api.childProfile()
      (feed, subscriptions) = try await (feedLoad, subscriptionLoad)
      discoveries = (try? await discoveryLoad) ?? []
      profile = try await profileLoad
    } catch {
      errorMessage = error.localizedDescription
    }
  }

  func channel(id: String) async throws -> Components.Schemas.ChannelPage? {
    #if DEBUG
      if isPreviewMode, let channel = Self.previewChannels.first(where: { $0.id == id }) {
        return Components.Schemas.ChannelPage(
          channel: channel,
          state: .allowed,
          subscribed: isSubscribed(to: id),
          videos: (Self.previewVideos + Self.previewShorts).filter { $0.channelId == id }
        )
      }
      if isPreviewMode,
        let discovery = Self.previewDiscoveries.first(where: { $0.video.channelId == id })
      {
        return Components.Schemas.ChannelPage(
          channel: Components.Schemas.Channel(
            id: id,
            title: discovery.video.channelTitle ?? "New channel"
          ),
          state: .requestable,
          subscribed: false,
          pendingRequest: discovery.pendingRequest,
          videos: []
        )
      }
    #endif
    return try await api?.childChannel(id: id)
  }

  func search(query: String) async throws -> Components.Schemas.SearchResults? {
    try await api?.searchForChild(query: query)
  }

  func video(id: String) async throws -> Components.Schemas.WatchPage? {
    #if DEBUG
      if isPreviewMode,
        let video = (Self.previewVideos + Self.previewShorts).first(where: { $0.id == id })
      {
        return Components.Schemas.WatchPage(
          video: video,
          embedUrl: "https://www.youtube-nocookie.com/embed/\(video.id)",
          autoplay: true,
          shareUrl: "https://www.youtube.com/watch?v=\(video.id)"
        )
      }
    #endif
    return try await api?.childVideo(id: id)
  }

  func watchNext(excluding videoID: String, limit: Int = 12) -> [Components.Schemas.Video] {
    Array(feed.lazy.filter { $0.id != videoID && !$0.isShort }.prefix(limit))
  }

  func discoverNext(excluding videoID: String, limit: Int = 4) -> [Components.Schemas.Discovery] {
    Array(discoveries.lazy.filter { $0.video.id != videoID }.prefix(limit))
  }

  func shorts(session: String, offset: Int, limit: Int = 8) async throws
    -> [Components.Schemas.Video]
  {
    #if DEBUG
      if isPreviewMode {
        return (0..<limit).map { Self.previewShorts[(offset + $0) % Self.previewShorts.count] }
      }
    #endif
    return try await api?.childShorts(session: session, offset: offset, limit: limit) ?? []
  }

  func setSubscribed(_ subscribed: Bool, channelID: String) async throws {
    guard let api else { return }
    try await api.setSubscribed(subscribed, channelID: channelID)
    subscriptions = try await api.childSubscriptions()
  }

  func isSubscribed(to channelID: String) -> Bool {
    subscriptions.contains { $0.id == channelID }
  }

  func setReaction(_ reaction: ChildReaction?, videoID: String) async throws {
    try await api?.setVideoReaction(reaction, videoID: videoID)
  }

  func recordWatch(videoID: String, startedAt: Date, secondsWatched: Int) async {
    try? await api?.recordVideoWatch(
      videoID: videoID,
      startedAt: startedAt,
      secondsWatched: secondsWatched
    )
  }

  func updatePlayback(videoID: String, state: PlaybackLeaseState) async -> Bool {
    #if DEBUG
      if isPreviewMode { return true }
    #endif
    guard let api else { return true }
    do {
      return try await api.updatePlayback(videoID: videoID, state: state)
    } catch {
      // A transient heartbeat failure must not interrupt a child mid-video.
      return true
    }
  }

  func requestChannel(channelID: String, videoID: String? = nil) async throws {
    _ = try await api?.requestChannel(channelID: channelID, promptedByVideoID: videoID)
  }

  func requests() async throws -> [Components.Schemas.Request] {
    try await api?.childRequests() ?? []
  }

  func unpair() {
    tokenStore.delete()
    api = nil
    profile = nil
    feed = []
    discoveries = []
    subscriptions = []
    destination = .pairing
  }

  private func perform(_ operation: () async throws -> Void) async {
    isWorking = true
    errorMessage = nil
    defer { isWorking = false }
    do {
      try await operation()
    } catch {
      errorMessage = error.localizedDescription
    }
  }

  #if DEBUG
    private static let previewChannels = [
      Components.Schemas.Channel(
        id: "science", title: "Crash Course Kids", subscriberCount: 910_000),
      Components.Schemas.Channel(
        id: "animals", title: "Brave Wilderness", subscriberCount: 21_500_000),
      Components.Schemas.Channel(
        id: "build", title: "Art for Kids Hub", subscriberCount: 9_300_000),
    ]

    private static let previewVideos = [
      Components.Schemas.Video(
        id: "volcanoes",
        channelId: "science",
        channelTitle: "Crash Course Kids",
        title: "Why Do Volcanoes Erupt?",
        durationSeconds: 542,
        publishedAt: Date(timeIntervalSinceNow: -86_400 * 12),
        isShort: false
      ),
      Components.Schemas.Video(
        id: "octopus",
        channelId: "animals",
        channelTitle: "Brave Wilderness",
        title: "Meeting the Cleverest Octopus in the Ocean",
        durationSeconds: 781,
        publishedAt: Date(timeIntervalSinceNow: -86_400 * 4),
        isShort: false
      ),
      Components.Schemas.Video(
        id: "dragon",
        channelId: "build",
        channelTitle: "Art for Kids Hub",
        title: "Draw a Friendly Dragon Step by Step",
        durationSeconds: 665,
        publishedAt: Date(timeIntervalSinceNow: -86_400 * 20),
        isShort: false
      ),
    ]

    private static let previewDiscoveries = [
      Components.Schemas.Discovery(
        video: Components.Schemas.Video(
          id: "reef-builders",
          channelId: "new-ocean-channel",
          channelTitle: "Ocean Lab",
          title: "How Tiny Animals Build a Coral Reef",
          durationSeconds: 618,
          publishedAt: Date(timeIntervalSinceNow: -86_400 * 3),
          isShort: false,
          locked: true
        ),
        reason: "Because you liked Meeting the Cleverest Octopus in the Ocean",
        pendingRequest: false
      ),
      Components.Schemas.Discovery(
        video: Components.Schemas.Video(
          id: "lava-lab",
          channelId: "new-earth-channel",
          channelTitle: "Earthworks",
          title: "Build a Safe Mini Lava Flow",
          durationSeconds: 492,
          publishedAt: Date(timeIntervalSinceNow: -86_400 * 8),
          isShort: false,
          locked: true
        ),
        reason: "Because you finished Why Do Volcanoes Erupt?",
        pendingRequest: true
      ),
    ]

    private static let previewShorts = [
      Components.Schemas.Video(
        id: "tiny-volcano",
        channelId: "science",
        channelTitle: "Crash Course Kids",
        title: "A volcano makes its own lightning",
        durationSeconds: 42,
        isShort: true
      ),
      Components.Schemas.Video(
        id: "octopus-dream",
        channelId: "animals",
        channelTitle: "Brave Wilderness",
        title: "Yes, octopuses change color while they dream",
        durationSeconds: 58,
        isShort: true
      ),
      Components.Schemas.Video(
        id: "dragon-trick",
        channelId: "build",
        channelTitle: "Art for Kids Hub",
        title: "The easiest trick for drawing dragon wings",
        durationSeconds: 37,
        isShort: true
      ),
    ]
  #endif
}
