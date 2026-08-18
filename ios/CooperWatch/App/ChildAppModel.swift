import CoopKit
import Observation
import SwiftUI
import UIKit

@MainActor
@Observable
final class ChildAppModel {
  struct ChannelSearchRequest: Equatable {
    let id = UUID()
    let channelID: String
    let channelTitle: String
  }

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
  var channelSearchRequest: ChannelSearchRequest?
  private(set) var isPreviewMode = false
  private(set) var showsPlayerLoadingPreview = false

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
        showsPlayerLoadingPreview =
          ProcessInfo.processInfo.environment["COOP_UI_PLAYER_LOADING"] == "1"
        serverAddress = "https://coop.example"
        profile = Components.Schemas.ChildProfile(
          id: "preview-child",
          name: "Cooper",
          shortsEnabled: ProcessInfo.processInfo.environment["COOP_UI_SHORTS_DISABLED"] != "1",
          watchPageAutoplay: false,
          videoSearchTiles: true,
          channelDiscoveryEnabled: true,
          webLinkingEnabled: true,
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
            installUrl:
              "itms-services://?action=download-manifest&url=https://coop.example/install/child.plist",
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
    enablePushNotificationsIfNeeded()
    startRequestWatcher()
  }

  func approveWebLink(_ scannedValue: String) async throws {
    guard let api else { throw CoopAPIError.invalidSession }
    let payload = try WebDeviceLinkPayload(scannedValue: scannedValue)
    let configuredServer = try ServerURL.normalize(serverAddress)
    guard payload.serverURL.scheme == configuredServer.scheme,
      payload.serverURL.host == configuredServer.host,
      payload.serverURL.port == configuredServer.port
    else {
      throw WebDeviceLinkError.differentServer
    }
    try await api.approveWebLinkAsChild(
      linkID: payload.id,
      approvalToken: payload.approvalToken
    )
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

  func search(query: String, channelID: String? = nil) async throws
    -> Components.Schemas.SearchResults?
  {
    #if DEBUG
      if isPreviewMode {
        let videos = (Self.previewVideos + Self.previewShorts).filter { video in
          (channelID == nil || video.channelId == channelID)
            && video.title.localizedCaseInsensitiveContains(query)
        }
        return Components.Schemas.SearchResults(channels: [], videos: videos)
      }
    #endif
    return try await api?.searchForChild(query: query, channelID: channelID)
  }

  func openSearch(channelID: String, channelTitle: String) {
    channelSearchRequest = ChannelSearchRequest(
      channelID: channelID,
      channelTitle: channelTitle
    )
  }

  // The embed wrapper anchors at this origin so YouTube receives a real
  // referrer; see YouTubeEmbedRequest.wrapperHTML.
  var playbackOrigin: URL? {
    try? ServerURL.normalize(serverAddress)
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

  func watchNext(excluding videoID: String, limit: Int = 12) async -> [Components.Schemas.Video] {
    // A fresh fetch draws a new exploration seed on the server, so each watch
    // page offers a different mix instead of the launch-time feed's top rows.
    if let api, let fresh = try? await api.childFeed(limit: limit + 1) {
      return watchNextSlice(from: fresh, excluding: videoID, limit: limit)
    }
    return watchNextSlice(from: feed, excluding: videoID, limit: limit)
  }

  private func watchNextSlice(
    from videos: [Components.Schemas.Video],
    excluding videoID: String,
    limit: Int
  ) -> [Components.Schemas.Video] {
    Array(videos.lazy.filter { $0.id != videoID && !$0.isShort }.prefix(limit))
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

  // MARK: - Live refresh

  // A decided request should just appear, but a feed that reorders while a
  // video or Short is playing reads as the app glitching. Playback surfaces
  // count themselves in and out, and refreshes wait for calm.
  private var activePlaybackCount = 0
  private var pendingLibraryRefresh = false
  private var pushRegistrationRequested = false
  private var requestStatuses: [String: Components.Schemas.RequestStatus] = [:]
  private var requestWatcher: Task<Void, Never>?
  private var leftForegroundAt: Date?

  private static let requestPollInterval: Duration = .seconds(20)
  private static let staleForegroundAge: TimeInterval = 5 * 60

  func playbackDidStart() {
    activePlaybackCount += 1
  }

  func playbackDidStop() {
    activePlaybackCount = max(0, activePlaybackCount - 1)
    guard activePlaybackCount == 0, pendingLibraryRefresh else { return }
    pendingLibraryRefresh = false
    Task { await loadLibrary() }
  }

  func refreshLibraryWhenCalm() {
    if activePlaybackCount > 0 {
      pendingLibraryRefresh = true
    } else {
      Task { await loadLibrary() }
    }
  }

  func sceneBecameActive() async {
    await checkForRequiredUpdate()
    guard destination == .watch else { return }
    enablePushNotificationsIfNeeded()
    startRequestWatcher()

    // A quick app switch keeps the feed put; a real absence earns a fresh one.
    let wasAwayLong = leftForegroundAt.map {
      Date.now.timeIntervalSince($0) > Self.staleForegroundAge
    } ?? false
    leftForegroundAt = nil
    await checkRequests(refreshLibraryAnyway: wasAwayLong)
  }

  func sceneWentInactive() {
    leftForegroundAt = .now
    requestWatcher?.cancel()
    requestWatcher = nil
  }

  func enablePushNotificationsIfNeeded() {
    guard !isPreviewMode, !pushRegistrationRequested, api != nil else { return }
    pushRegistrationRequested = true
    PushRegistration.onToken = { [weak self] token in
      guard let api = self?.api else { return }
      Task { try? await api.registerChildPushToken(token) }
    }
    PushRegistration.onNotification = { [weak self] in
      Task { await self?.checkRequests(refreshLibraryAnyway: true) }
    }
    PushRegistration.register()
  }

  private func startRequestWatcher() {
    guard requestWatcher == nil, !isPreviewMode else { return }
    requestWatcher = Task { [weak self] in
      while !Task.isCancelled {
        try? await Task.sleep(for: Self.requestPollInterval)
        guard !Task.isCancelled else { return }
        await self?.checkRequests(refreshLibraryAnyway: false)
      }
    }
  }

  private func checkRequests(refreshLibraryAnyway: Bool) async {
    guard api != nil else { return }
    guard let current = try? await requests() else { return }

    let previous = requestStatuses
    requestStatuses = Dictionary(
      uniqueKeysWithValues: current.map { ($0.id, $0.status) }
    )
    let decided = current.contains { request in
      previous[request.id] == .pending && request.status != .pending
    }
    if decided || refreshLibraryAnyway {
      refreshLibraryWhenCalm()
    }
  }

  func unpair() {
    tokenStore.delete()
    api = nil
    profile = nil
    feed = []
    discoveries = []
    subscriptions = []
    destination = .pairing
    requestWatcher?.cancel()
    requestWatcher = nil
    requestStatuses = [:]
    pendingLibraryRefresh = false
    // The server-side push registration dies with the device pairing row.
    pushRegistrationRequested = false
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
