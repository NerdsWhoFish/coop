import CoopKit
import Foundation
import Observation

@MainActor
@Observable
final class AppModel {
  struct PendingAuthentication {
    let challenge: String
    let secret: String?
    let provisioningURL: String?
  }

  enum Destination {
    case connecting
    case authentication(needsSetup: Bool)
    case totp(PendingAuthentication)
    case dashboard
  }

  var destination: Destination = .connecting
  var serverAddress: String
  var parent: Components.Schemas.Parent?
  var requests: [Components.Schemas.Request] = []
  var activePlayback: [Components.Schemas.Playback] = []
  var children: [Components.Schemas.Child] = []
  var isWorking = false
  var errorMessage: String?

  private var api: CoopAPI?
  private let credentials = SecureTokenStore(
    service: "fish.nerdswhofish.coop.parent",
    account: "parent-session"
  )
  private let defaults = UserDefaults.standard
  private static let serverKey = "coop.server.address"

  private var previewChannelWeights = [
    "science": 1,
    "drawing": 0,
    "outdoors": -1,
  ]

  static var uiPreviewScreen: String? {
    #if DEBUG
      return ProcessInfo.processInfo.environment["COOP_UI_SCREEN"]
    #else
      return nil
    #endif
  }

  static var isUIPreview: Bool { uiPreviewScreen != nil }
  static var showsRecommendationPreview: Bool { uiPreviewScreen == "recommendations" }
  static var showsFamilyPreview: Bool { uiPreviewScreen == "family" }
  static var showsRequestsPreview: Bool { uiPreviewScreen == "requests" }
  static var showsChildSettingsPreview: Bool { uiPreviewScreen == "child-settings" }

  static let recommendationPreviewChild = Components.Schemas.Child(
    value1: .init(id: "preview-child", name: "River", deviceCount: 2, pendingRequestCount: 1),
    value2: .init(
      shortsEnabled: true,
      watchPageAutoplay: false,
      videoSearchTiles: true,
      channelDiscoveryEnabled: true,
      dailySearchLimit: 12
    )
  )

  static let familyPreview = Components.Schemas.Family(
    id: "preview-family",
    name: "Stouts",
    timezone: "America/New_York",
    apiKeyConfigured: true
  )

  static let familyPreviewQuota = [
    Components.Schemas.QuotaStatus(purpose: .feed, used: 11, budget: 8_000),
    Components.Schemas.QuotaStatus(purpose: .search, used: 2, budget: 90),
    Components.Schemas.QuotaStatus(purpose: .backfill, used: 0, budget: 500),
  ]

  static let familyPreviewParent = Components.Schemas.Parent(
    id: "preview-parent",
    email: "joey@theoutdoorprogrammer.com",
    role: .admin,
    totpEnrolled: true
  )

  init() {
    serverAddress = defaults.string(forKey: Self.serverKey) ?? ""
    if Self.showsFamilyPreview {
      parent = Self.familyPreviewParent
    } else if Self.showsRequestsPreview {
      let channel = Components.Schemas.Channel(
        id: "UCpreview",
        title: "Build It Together",
        youtubeUrl: "https://www.youtube.com/channel/UCpreview"
      )
      let video = Components.Schemas.Video(
        id: "preview-video",
        channelId: channel.id,
        channelTitle: channel.title,
        title: "A Castle Made from Cardboard",
        durationSeconds: 486,
        isShort: false
      )
      parent = Self.familyPreviewParent
      requests = [
        Components.Schemas.Request(
          id: "preview-request",
          childId: "preview-child",
          childName: "River",
          channel: channel,
          promptedByVideo: video,
          status: .pending,
          createdAt: .now.addingTimeInterval(-90)
        )
      ]
      activePlayback = [
        Components.Schemas.Playback(
          childId: "preview-child",
          childName: "River",
          video: video,
          startedAt: .now.addingTimeInterval(-30)
        )
      ]
      destination = .dashboard
    }
  }

  func restore() async {
    guard !serverAddress.isEmpty else {
      return
    }
    await connect(usingStoredSession: true)
  }

  func connect(usingStoredSession: Bool = false) async {
    await perform {
      let serverURL = try ServerURL.normalize(serverAddress)
      defaults.set(serverAddress, forKey: Self.serverKey)

      let publicAPI = CoopAPI(serverURL: serverURL)
      let needsSetup = try await publicAPI.setupStatus()
      if usingStoredSession, let token = credentials.load() {
        let authenticatedAPI = CoopAPI(serverURL: serverURL, token: token)
        do {
          parent = try await authenticatedAPI.currentParent()
          api = authenticatedAPI
          destination = .dashboard
          await loadDashboard()
          return
        } catch CoopAPIError.invalidSession {
          credentials.delete()
        }
      }

      api = publicAPI
      destination = .authentication(needsSetup: needsSetup)
    }
  }

  func logIn(email: String, password: String) async {
    await perform {
      guard let api else { return }
      let challenge = try await api.logIn(email: email, password: password)
      continueAuthentication(challenge)
    }
  }

  func acceptInvitation(code: String, password: String) async throws {
    guard let api else { return }
    let challenge = try await api.acceptInvitation(code: code, password: password)
    continueAuthentication(challenge)
  }

  func setUp(familyName: String, email: String, password: String) async {
    await perform {
      guard let api else { return }
      let timezone = TimeZone.current.identifier
      let challenge = try await api.setUpFamily(
        familyName: familyName,
        timezone: timezone,
        email: email,
        password: password
      )
      continueAuthentication(challenge)
    }
  }

  func verifyTOTP(_ code: String, challenge: PendingAuthentication) async {
    await perform {
      guard let api else { return }
      let session = try await api.verifyTOTP(challenge: challenge.challenge, code: code)
      try activate(session)
      await loadDashboard()
    }
  }

  func loadDashboard() async {
    async let requestLoad: Void = loadRequests()
    async let childLoad: Void = loadChildren()
    _ = await (requestLoad, childLoad)
  }

  func loadRequests() async {
    guard let api else { return }
    do {
      requests = try await api.pendingRequests()
    } catch {
      errorMessage = error.localizedDescription
    }
  }

  func loadChildren() async {
    guard let api else { return }
    do {
      children = try await api.children()
    } catch {
      errorMessage = error.localizedDescription
    }
  }

  func monitorPlayback() async {
    guard let api else { return }
    var cursor: String?
    while !Task.isCancelled {
      do {
        let page = try await api.activePlayback(cursor: cursor)
        activePlayback = page.items
        cursor = page.cursor
      } catch is CancellationError {
        return
      } catch {
        if !Task.isCancelled {
          try? await Task.sleep(for: .seconds(3))
        }
      }
    }
  }

  func setVideoBlocked(_ blocked: Bool, videoID: String, childID: String) async throws {
    guard let api else { return }
    try await api.setVideoBlocked(blocked, videoID: videoID, childID: childID)
    if blocked {
      activePlayback.removeAll { $0.childId == childID && $0.video.id == videoID }
    }
  }

  func videoBlocks(childID: String) async throws -> [Components.Schemas.VideoBlock] {
    guard let api else { return [] }
    return try await api.videoBlocks(childID: childID)
  }

  func createChild(name: String) async throws {
    guard let api else { return }
    let child = try await api.createChild(name: name)
    children.append(child)
  }

  func updateChild(
    id: String,
    settings: Components.Schemas.ChildSettings
  ) async throws {
    guard let api else { return }
    let updated = try await api.updateChild(id: id, settings: settings)
    if let index = children.firstIndex(where: { $0.value1.id == id }) {
      children[index] = updated
    }
  }

  func deleteChild(id: String) async throws {
    guard let api else { return }
    try await api.deleteChild(id: id)
    children.removeAll { $0.value1.id == id }
  }

  func createPairingCode(childID: String) async throws -> Components.Schemas.PairingCode? {
    guard let api else { return nil }
    return try await api.createPairingCode(childID: childID)
  }

  func childDevices(childID: String) async throws -> [Components.Schemas.Device] {
    guard let api else { return [] }
    return try await api.childDevices(childID: childID)
  }

  func revokeChildDevice(id: String) async throws {
    guard let api else { return }
    try await api.revokeChildDevice(id: id)
  }

  func globalAllowlist() async throws -> [Components.Schemas.ApprovedChannel] {
    guard let api else { return [] }
    return try await api.globalAllowlist()
  }

  func childAllowlist(childID: String) async throws -> [Components.Schemas.EffectiveChannel] {
    guard let api else { return [] }
    return try await api.childAllowlist(childID: childID)
  }

  func feedRecommendations(childID: String) async throws -> [FeedRecommendation] {
    if Self.showsRecommendationPreview {
      return Self.previewRecommendations.sorted { left, right in
        (previewChannelWeights[left.channelID] ?? 0) > (previewChannelWeights[right.channelID] ?? 0)
      }
    }
    guard let api else { return [] }
    let page = try await api.childRecommendations(childID: childID)
    return page.items.map { item in
      FeedRecommendation(
        id: item.video.id,
        channelID: item.video.channelId,
        channelTitle: item.video.channelTitle ?? "Approved channel",
        title: item.video.title,
        thumbnailURL: item.video.thumbnailUrl.flatMap(URL.init(string:)),
        reason: item.reason,
        signal: RecommendationSignal(item.reasonKind)
      )
    }
  }

  func tunableChannels(childID: String) async throws -> [TunableChannel] {
    if Self.showsRecommendationPreview { return Self.previewChannels }
    let channels = try await childAllowlist(childID: childID)
    return channels.compactMap { entry in
      guard !(entry.value2.deniedForChild ?? false) else { return nil }
      let channel = entry.value1.value1
      return TunableChannel(
        id: channel.id,
        title: channel.title,
        thumbnailURL: channel.thumbnailUrl.flatMap(URL.init(string:))
      )
    }
  }

  func recommendationChannelWeights(childID: String) async throws -> [String: Int] {
    if Self.showsRecommendationPreview { return previewChannelWeights }
    guard let api else { return [:] }
    return Dictionary(
      uniqueKeysWithValues: try await api.childChannelWeights(childID: childID)
        .map { ($0.channelId, $0.weight) }
    )
  }

  func setRecommendationChannelWeight(_ weight: Int, channelID: String, childID: String)
    async throws
  {
    if Self.showsRecommendationPreview {
      previewChannelWeights[channelID] = weight
      return
    }
    guard let api else { return }
    try await api.setChildChannelWeight(weight, channelID: channelID, childID: childID)
  }

  func blocklist() async throws -> [Components.Schemas.BlockedChannel] {
    guard let api else { return [] }
    return try await api.blocklist()
  }

  func keywords(childID: String?) async throws -> [Components.Schemas.Keyword] {
    guard let api else { return [] }
    return try await api.keywords(childID: childID)
  }

  func searchChannels(query: String) async throws -> [Components.Schemas.Channel] {
    guard let api else { return [] }
    return try await api.searchChannels(query: query)
  }

  func allowChannel(_ channelID: String, childID: String?) async throws {
    guard let api else { return }
    try await api.allowChannel(channelID, childID: childID)
  }

  func removeChannel(_ channelID: String, childID: String?) async throws {
    guard let api else { return }
    try await api.removeChannel(channelID, childID: childID)
  }

  func setChannelDenied(_ denied: Bool, channelID: String, childID: String) async throws {
    guard let api else { return }
    try await api.setChannelDenied(denied, channelID: channelID, childID: childID)
  }

  func setChannelBlocked(_ blocked: Bool, channelID: String, reason: String? = nil) async throws {
    guard let api else { return }
    try await api.setChannelBlocked(blocked, channelID: channelID, reason: reason)
  }

  func createKeyword(
    term: String,
    childID: String?,
    matchTitle: Bool,
    matchTags: Bool,
    matchDescription: Bool,
    wholeWord: Bool
  ) async throws {
    guard let api else { return }
    _ = try await api.createKeyword(
      term: term,
      childID: childID,
      matchTitle: matchTitle,
      matchTags: matchTags,
      matchDescription: matchDescription,
      wholeWord: wholeWord
    )
  }

  func deleteKeyword(id: String) async throws {
    guard let api else { return }
    try await api.deleteKeyword(id: id)
  }

  func suppressions(childID: String) async throws -> [Components.Schemas.Suppression] {
    guard let api else { return [] }
    return try await api.suppressions(childID: childID)
  }

  func overrideSuppression(id: String, familyWide: Bool) async throws {
    guard let api else { return }
    try await api.overrideSuppression(id: id, familyWide: familyWide)
  }

  func family() async throws -> Components.Schemas.Family? {
    if Self.showsFamilyPreview { return Self.familyPreview }
    guard let api else { return nil }
    return try await api.family()
  }

  func auditEvents() async throws -> [Components.Schemas.AuditEvent] {
    guard let api else { return [] }
    return try await api.auditEvents()
  }

  func deleteFamily() async throws {
    guard let api else { return }
    try await api.deleteFamily()
    logOut()
  }

  func familyQuota() async throws -> [Components.Schemas.QuotaStatus] {
    if Self.showsFamilyPreview { return Self.familyPreviewQuota }
    guard let api else { return [] }
    return try await api.familyQuota()
  }

  func setFamilyAPIKey(_ apiKey: String) async throws {
    guard let api else { return }
    try await api.setFamilyAPIKey(apiKey)
  }

  func parents() async throws -> [Components.Schemas.Parent] {
    if Self.showsFamilyPreview { return [Self.familyPreviewParent] }
    guard let api else { return [] }
    return try await api.parents()
  }

  func inviteParent(email: String, admin: Bool, childIDs: [String]) async throws
    -> Components.Schemas.Invitation?
  {
    guard let api else { return nil }
    return try await api.inviteParent(email: email, admin: admin, childIDs: childIDs)
  }

  func setParentScope(parentID: String, childIDs: [String]) async throws {
    guard let api else { return }
    try await api.setParentScope(parentID: parentID, childIDs: childIDs)
  }

  func deleteParent(id: String) async throws {
    guard let api else { return }
    try await api.deleteParent(id: id)
  }

  func approve(requestID: String, globally: Bool) async throws {
    guard let api else { return }
    try await api.approveRequest(id: requestID, globally: globally)
  }

  func deny(requestID: String, blockChannel: Bool) async throws {
    guard let api else { return }
    try await api.denyRequest(id: requestID, blockChannel: blockChannel)
  }

  func dismiss(requestID: String) {
    requests.removeAll { $0.id == requestID }
  }

  func logOut() {
    credentials.delete()
    api = nil
    parent = nil
    requests = []
    activePlayback = []
    children = []
    destination = .connecting
  }

  private func activate(_ session: Components.Schemas.Session) throws {
    try credentials.save(session.token)
    let serverURL = try ServerURL.normalize(serverAddress)
    api = CoopAPI(serverURL: serverURL, token: session.token)
    parent = session.parent
    destination = .dashboard
  }

  private func continueAuthentication(_ challenge: Components.Schemas.AuthChallenge) {
    destination = .totp(
      PendingAuthentication(
        challenge: challenge.challenge,
        secret: challenge.enrollment?.secret,
        provisioningURL: challenge.enrollment?.provisioningUrl
      ))
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

  private static let previewChannels = [
    TunableChannel(id: "science", title: "Deep Sea Lab", thumbnailURL: nil),
    TunableChannel(id: "drawing", title: "Draw Every Day", thumbnailURL: nil),
    TunableChannel(id: "outdoors", title: "Trail Kids", thumbnailURL: nil),
    TunableChannel(id: "music", title: "Tiny Orchestra", thumbnailURL: nil),
  ]

  private static let previewRecommendations = [
    FeedRecommendation(
      id: "anglerfish",
      channelID: "science",
      channelTitle: "Deep Sea Lab",
      title: "Why anglerfish glow in the dark",
      thumbnailURL: nil,
      reason: "You asked Coop to show more from this channel.",
      signal: .parentMore
    ),
    FeedRecommendation(
      id: "owl",
      channelID: "drawing",
      channelTitle: "Draw Every Day",
      title: "Draw a snowy owl in ten shapes",
      thumbnailURL: nil,
      reason: "They chose to watch this video more than once.",
      signal: .rewatched
    ),
    FeedRecommendation(
      id: "camp",
      channelID: "outdoors",
      channelTitle: "Trail Kids",
      title: "Build a tiny camp stove safely",
      thumbnailURL: nil,
      reason: "You asked Coop to show this channel less often.",
      signal: .parentLess
    ),
    FeedRecommendation(
      id: "cello",
      channelID: "music",
      channelTitle: "Tiny Orchestra",
      title: "Meet the cello",
      thumbnailURL: nil,
      reason: "Something new from an approved channel.",
      signal: .unwatched
    ),
  ]
}
