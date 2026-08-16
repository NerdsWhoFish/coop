import CoopKit
import Observation
import SwiftUI
import UIKit

@MainActor
@Observable
final class ChildAppModel {
  enum Destination: Equatable {
    case pairing
    case watch
  }

  var destination: Destination = .pairing
  var serverAddress: String
  var profile: Components.Schemas.ChildProfile?
  var feed: [Components.Schemas.Video] = []
  var subscriptions: [Components.Schemas.Channel] = []
  var isWorking = false
  var errorMessage: String?

  private var api: CoopAPI?
  private let defaults = UserDefaults.standard
  private let tokenStore = SecureTokenStore(
    service: "fish.nerdswhofish.coop.child",
    account: "device-token"
  )
  private static let serverKey = "coop.child.server.address"

  init() {
    serverAddress = defaults.string(forKey: Self.serverKey) ?? ""
  }

  func restore() async {
    guard !serverAddress.isEmpty, let token = tokenStore.load() else { return }
    await perform {
      let serverURL = try ServerURL.normalize(serverAddress)
      let authenticatedAPI = CoopAPI(serverURL: serverURL, token: token)
      do {
        profile = try await authenticatedAPI.childProfile()
      } catch CoopAPIError.invalidSession {
        tokenStore.delete()
        return
      }
      api = authenticatedAPI
      destination = .watch
      await loadLibrary()
    }
  }

  func pair(code: String) async {
    await perform {
      let serverURL = try ServerURL.normalize(serverAddress)
      defaults.set(serverAddress, forKey: Self.serverKey)
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
      (feed, subscriptions) = try await (feedLoad, subscriptionLoad)
    } catch {
      errorMessage = error.localizedDescription
    }
  }

  func channel(id: String) async throws -> Components.Schemas.ChannelPage? {
    try await api?.childChannel(id: id)
  }

  func search(query: String) async throws -> Components.Schemas.SearchResults? {
    try await api?.searchForChild(query: query)
  }

  func video(id: String) async throws -> Components.Schemas.WatchPage? {
    try await api?.childVideo(id: id)
  }

  func setSubscribed(_ subscribed: Bool, channelID: String) async throws {
    guard let api else { return }
    try await api.setSubscribed(subscribed, channelID: channelID)
    subscriptions = try await api.childSubscriptions()
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
}
