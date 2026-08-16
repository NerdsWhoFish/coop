import Foundation
import HTTPTypes
import OpenAPIRuntime
import OpenAPIURLSession

public actor CoopAPI {
  private let client: Client

  public init(serverURL: URL, token: String? = nil) {
    let middlewares: [any ClientMiddleware]
    if let token {
      middlewares = [BearerAuthorizationMiddleware(token: token)]
    } else {
      middlewares = []
    }
    client = Client(
      serverURL: serverURL,
      transport: URLSessionTransport(),
      middlewares: middlewares
    )
  }

  public func setupStatus() async throws -> Bool {
    let output = try await client.getSetupStatus()
    guard case .ok(let response) = output else {
      throw CoopAPIError.unexpectedResponse
    }
    return try response.body.json.needsSetup
  }

  public func setUpFamily(
    familyName: String,
    timezone: String,
    email: String,
    password: String
  ) async throws -> Components.Schemas.AuthChallenge {
    let request = Components.Schemas.SetupRequest(
      familyName: familyName,
      timezone: timezone,
      email: email,
      password: password
    )
    let output = try await client.setUpFamily(body: .json(request))
    guard case .created(let response) = output else {
      throw CoopAPIError.unexpectedResponse
    }
    return try response.body.json
  }

  public func logIn(email: String, password: String) async throws
    -> Components.Schemas.AuthChallenge
  {
    let credentials = Operations.LogInParent.Input.Body.JsonPayload(
      email: email,
      password: password
    )
    let output = try await client.logInParent(body: .json(credentials))
    switch output {
    case .ok(let response):
      return try response.body.json
    case .unauthorized:
      throw CoopAPIError.invalidCredentials
    default:
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func acceptInvitation(code: String, password: String) async throws
    -> Components.Schemas.AuthChallenge
  {
    let payload = Operations.AcceptParentInvitation.Input.Body.JsonPayload(
      code: code,
      password: password
    )
    let output = try await client.acceptParentInvitation(body: .json(payload))
    switch output {
    case .created(let response):
      return try response.body.json
    case .unauthorized:
      throw CoopAPIError.invalidInvitation
    default:
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func verifyTOTP(challenge: String, code: String) async throws
    -> Components.Schemas.Session
  {
    let payload = Operations.VerifyParentTOTP.Input.Body.JsonPayload(
      challenge: challenge,
      code: code
    )
    let output = try await client.verifyParentTOTP(body: .json(payload))
    switch output {
    case .ok(let response):
      return try response.body.json
    case .unauthorized:
      throw CoopAPIError.invalidCredentials
    default:
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func currentParent() async throws -> Components.Schemas.Parent {
    let output = try await client.getCurrentParent()
    switch output {
    case .ok(let response):
      return try response.body.json
    case .unauthorized:
      throw CoopAPIError.invalidSession
    default:
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func family() async throws -> Components.Schemas.Family {
    let output = try await client.getFamily()
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func familyQuota() async throws -> [Components.Schemas.QuotaStatus] {
    let output = try await client.getFamilyQuota()
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func setFamilyAPIKey(_ apiKey: String) async throws {
    let payload = Operations.SetFamilyAPIKey.Input.Body.JsonPayload(apiKey: apiKey)
    guard case .noContent = try await client.setFamilyAPIKey(body: .json(payload)) else {
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func parents() async throws -> [Components.Schemas.Parent] {
    let output = try await client.listParents()
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func inviteParent(
    email: String,
    admin: Bool,
    childIDs: [String]
  ) async throws -> Components.Schemas.Invitation {
    let payload = Operations.InviteParent.Input.Body.JsonPayload(
      email: email,
      role: admin ? .admin : .parent,
      childIds: admin ? [] : childIDs
    )
    let output = try await client.inviteParent(body: .json(payload))
    guard case .created(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func setParentScope(parentID: String, childIDs: [String]) async throws {
    let path = Operations.SetParentScope.Input.Path(parentId: parentID)
    let payload = Operations.SetParentScope.Input.Body.JsonPayload(childIds: childIDs)
    guard case .noContent = try await client.setParentScope(path: path, body: .json(payload)) else {
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func deleteParent(id: String) async throws {
    let path = Operations.DeleteParent.Input.Path(parentId: id)
    guard case .noContent = try await client.deleteParent(path: path) else {
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func children() async throws -> [Components.Schemas.Child] {
    let output = try await client.listChildren()
    guard case .ok(let response) = output else {
      throw CoopAPIError.unexpectedResponse
    }
    return try response.body.json
  }

  @discardableResult
  public func createChild(name: String) async throws -> Components.Schemas.Child {
    let payload = Operations.CreateChild.Input.Body.JsonPayload(name: name)
    let output = try await client.createChild(body: .json(payload))
    guard case .created(let response) = output else {
      throw CoopAPIError.unexpectedResponse
    }
    return try response.body.json
  }

  @discardableResult
  public func updateChild(
    id: String,
    settings: Components.Schemas.ChildSettings
  ) async throws -> Components.Schemas.Child {
    let path = Operations.UpdateChild.Input.Path(childId: id)
    let output = try await client.updateChild(path: path, body: .json(settings))
    guard case .ok(let response) = output else {
      throw CoopAPIError.unexpectedResponse
    }
    return try response.body.json
  }

  public func deleteChild(id: String) async throws {
    let path = Operations.DeleteChild.Input.Path(childId: id)
    guard case .noContent = try await client.deleteChild(path: path) else {
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func createPairingCode(childID: String) async throws -> Components.Schemas.PairingCode {
    let path = Operations.CreatePairingCode.Input.Path(childId: childID)
    let output = try await client.createPairingCode(path: path)
    guard case .created(let response) = output else {
      throw CoopAPIError.unexpectedResponse
    }
    return try response.body.json
  }

  public func childDevices(childID: String) async throws -> [Components.Schemas.Device] {
    let path = Operations.ListChildDevices.Input.Path(childId: childID)
    let output = try await client.listChildDevices(path: path)
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func revokeChildDevice(id: String) async throws {
    let path = Operations.RevokeChildDevice.Input.Path(deviceId: id)
    guard case .noContent = try await client.revokeChildDevice(path: path) else {
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func globalAllowlist() async throws -> [Components.Schemas.ApprovedChannel] {
    let output = try await client.getGlobalAllowlist()
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func childAllowlist(childID: String) async throws -> [Components.Schemas.EffectiveChannel]
  {
    let path = Operations.GetChildAllowlist.Input.Path(childId: childID)
    let output = try await client.getChildAllowlist(path: path)
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func childRecommendations(
    childID: String,
    limit: Int = 30,
    cursor: String? = nil
  ) async throws -> Components.Schemas.RecommendationPage {
    let path = Operations.GetChildRecommendations.Input.Path(childId: childID)
    let query = Operations.GetChildRecommendations.Input.Query(limit: limit, cursor: cursor)
    let output = try await client.getChildRecommendations(path: path, query: query)
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func childChannelWeights(childID: String) async throws
    -> [Components.Schemas.ChannelWeight]
  {
    let path = Operations.GetChildChannelWeights.Input.Path(childId: childID)
    let output = try await client.getChildChannelWeights(path: path)
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func setChildChannelWeight(_ weight: Int, channelID: String, childID: String)
    async throws
  {
    let path = Operations.SetChildChannelWeight.Input.Path(
      childId: childID,
      channelId: channelID
    )
    let payload = Operations.SetChildChannelWeight.Input.Body.JsonPayload(weight: weight)
    guard
      case .noContent = try await client.setChildChannelWeight(path: path, body: .json(payload))
    else { throw CoopAPIError.unexpectedResponse }
  }

  public func blocklist() async throws -> [Components.Schemas.BlockedChannel] {
    let output = try await client.getChannelBlocklist()
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func keywords(childID: String? = nil) async throws -> [Components.Schemas.Keyword] {
    let query = Operations.ListKeywords.Input.Query(childId: childID)
    let output = try await client.listKeywords(query: query)
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func searchChannels(query: String) async throws -> [Components.Schemas.Channel] {
    let input = Operations.SearchChannelsForParent.Input.Query(q: query)
    let output = try await client.searchChannelsForParent(query: input)
    switch output {
    case .ok(let response):
      return try response.body.json
    case .tooManyRequests:
      throw CoopAPIError.searchBudgetExhausted
    default:
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func allowChannel(_ channelID: String, childID: String? = nil) async throws {
    if let childID {
      let path = Operations.AllowChannelForChild.Input.Path(childId: childID)
      let payload = Operations.AllowChannelForChild.Input.Body.JsonPayload(channelId: channelID)
      guard
        case .noContent = try await client.allowChannelForChild(path: path, body: .json(payload))
      else { throw CoopAPIError.unexpectedResponse }
    } else {
      let payload = Operations.AllowChannelGlobally.Input.Body.JsonPayload(channelId: channelID)
      guard case .noContent = try await client.allowChannelGlobally(body: .json(payload)) else {
        throw CoopAPIError.unexpectedResponse
      }
    }
  }

  public func removeChannel(_ channelID: String, childID: String? = nil) async throws {
    if let childID {
      let path = Operations.DisallowChannelForChild.Input.Path(
        childId: childID,
        channelId: channelID
      )
      guard case .noContent = try await client.disallowChannelForChild(path: path) else {
        throw CoopAPIError.unexpectedResponse
      }
    } else {
      let path = Operations.DisallowChannelGlobally.Input.Path(channelId: channelID)
      guard case .noContent = try await client.disallowChannelGlobally(path: path) else {
        throw CoopAPIError.unexpectedResponse
      }
    }
  }

  public func setChannelDenied(_ denied: Bool, channelID: String, childID: String) async throws {
    if denied {
      let path = Operations.DenyChannelForChild.Input.Path(childId: childID, channelId: channelID)
      guard case .noContent = try await client.denyChannelForChild(path: path) else {
        throw CoopAPIError.unexpectedResponse
      }
    } else {
      let path = Operations.RemoveChildChannelDenial.Input.Path(
        childId: childID,
        channelId: channelID
      )
      guard case .noContent = try await client.removeChildChannelDenial(path: path) else {
        throw CoopAPIError.unexpectedResponse
      }
    }
  }

  public func setChannelBlocked(_ blocked: Bool, channelID: String, reason: String? = nil)
    async throws
  {
    if blocked {
      let payload = Operations.BlockChannel.Input.Body.JsonPayload(
        channelId: channelID, reason: reason)
      guard case .noContent = try await client.blockChannel(body: .json(payload)) else {
        throw CoopAPIError.unexpectedResponse
      }
    } else {
      let path = Operations.UnblockChannel.Input.Path(channelId: channelID)
      guard case .noContent = try await client.unblockChannel(path: path) else {
        throw CoopAPIError.unexpectedResponse
      }
    }
  }

  @discardableResult
  public func createKeyword(
    term: String,
    childID: String?,
    matchTitle: Bool = true,
    matchTags: Bool = true,
    matchDescription: Bool = false,
    wholeWord: Bool = true
  ) async throws -> Components.Schemas.Keyword {
    let payload = Components.Schemas.KeywordInput(
      term: term,
      childId: childID,
      matchTitle: matchTitle,
      matchTags: matchTags,
      matchDescription: matchDescription,
      wholeWord: wholeWord
    )
    let output = try await client.createKeyword(body: .json(payload))
    guard case .created(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func deleteKeyword(id: String) async throws {
    let path = Operations.DeleteKeyword.Input.Path(keywordId: id)
    guard case .noContent = try await client.deleteKeyword(path: path) else {
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func suppressions(childID: String) async throws -> [Components.Schemas.Suppression] {
    let path = Operations.ListSuppressions.Input.Path(childId: childID)
    let output = try await client.listSuppressions(path: path)
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json.items
  }

  public func overrideSuppression(id: String, familyWide: Bool) async throws {
    let path = Operations.OverrideSuppression.Input.Path(suppressionId: id)
    let payload = Operations.OverrideSuppression.Input.Body.JsonPayload(
      scope: familyWide ? .family : .child
    )
    guard case .noContent = try await client.overrideSuppression(path: path, body: .json(payload))
    else { throw CoopAPIError.unexpectedResponse }
  }

  public func pairChildDevice(code: String, deviceName: String) async throws -> ChildPairing {
    let payload = Operations.PairChildDevice.Input.Body.JsonPayload(
      code: code,
      deviceName: deviceName
    )
    let output = try await client.pairChildDevice(body: .json(payload))
    switch output {
    case .ok(let response):
      let body = try response.body.json
      return ChildPairing(token: body.token, profile: body.child)
    case .badRequest:
      throw CoopAPIError.invalidPairingCode
    default:
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func childProfile() async throws -> Components.Schemas.ChildProfile {
    let output = try await client.getChildProfile()
    switch output {
    case .ok(let response):
      return try response.body.json
    case .unauthorized:
      throw CoopAPIError.invalidSession
    default:
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func childFeed() async throws -> [Components.Schemas.Video] {
    let output = try await client.getChildFeed()
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json.items
  }

  public func childShorts(
    session: String,
    offset: Int,
    limit: Int = 10
  ) async throws -> [Components.Schemas.Video] {
    let query = Operations.GetChildShorts.Input.Query(
      session: session,
      limit: limit,
      offset: offset
    )
    let output = try await client.getChildShorts(query: query)
    switch output {
    case .ok(let response):
      return try response.body.json.items
    case .notFound:
      throw CoopAPIError.shortsDisabled
    default:
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func childSubscriptions() async throws -> [Components.Schemas.Channel] {
    let output = try await client.listChildSubscriptions()
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func setSubscribed(_ subscribed: Bool, channelID: String) async throws {
    if subscribed {
      let path = Operations.SubscribeToChannel.Input.Path(channelId: channelID)
      guard case .noContent = try await client.subscribeToChannel(path: path) else {
        throw CoopAPIError.unexpectedResponse
      }
    } else {
      let path = Operations.UnsubscribeFromChannel.Input.Path(channelId: channelID)
      guard case .noContent = try await client.unsubscribeFromChannel(path: path) else {
        throw CoopAPIError.unexpectedResponse
      }
    }
  }

  public func childChannel(id: String) async throws -> Components.Schemas.ChannelPage {
    let path = Operations.GetChildChannel.Input.Path(channelId: id)
    let output = try await client.getChildChannel(path: path)
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func searchForChild(query: String) async throws -> Components.Schemas.SearchResults {
    let query = Operations.SearchForChild.Input.Query(q: query)
    let output = try await client.searchForChild(query: query)
    switch output {
    case .ok(let response):
      return try response.body.json
    case .tooManyRequests:
      throw CoopAPIError.searchBudgetExhausted
    default:
      throw CoopAPIError.unexpectedResponse
    }
  }

  public func childVideo(id: String) async throws -> Components.Schemas.WatchPage {
    let path = Operations.GetChildVideo.Input.Path(videoId: id)
    let output = try await client.getChildVideo(path: path)
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func setVideoReaction(_ reaction: ChildReaction?, videoID: String) async throws {
    if let reaction {
      let path = Operations.SetVideoReaction.Input.Path(videoId: videoID)
      let payload = Operations.SetVideoReaction.Input.Body.JsonPayload(
        kind: reaction == .like ? .like : .dislike
      )
      guard case .noContent = try await client.setVideoReaction(path: path, body: .json(payload))
      else { throw CoopAPIError.unexpectedResponse }
    } else {
      let path = Operations.ClearVideoReaction.Input.Path(videoId: videoID)
      guard case .noContent = try await client.clearVideoReaction(path: path) else {
        throw CoopAPIError.unexpectedResponse
      }
    }
  }

  public func recordVideoWatch(videoID: String, startedAt: Date, secondsWatched: Int) async throws {
    let path = Operations.RecordVideoWatch.Input.Path(videoId: videoID)
    let payload = Operations.RecordVideoWatch.Input.Body.JsonPayload(
      startedAt: startedAt,
      secondsWatched: secondsWatched
    )
    guard case .noContent = try await client.recordVideoWatch(path: path, body: .json(payload))
    else { throw CoopAPIError.unexpectedResponse }
  }

  public func childRequests() async throws -> [Components.Schemas.Request] {
    let output = try await client.listChildRequests()
    guard case .ok(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  @discardableResult
  public func requestChannel(
    channelID: String,
    promptedByVideoID: String? = nil
  ) async throws -> Components.Schemas.Request {
    let payload = Operations.CreateChildRequest.Input.Body.JsonPayload(
      channelId: channelID,
      promptedByVideoId: promptedByVideoID
    )
    let output = try await client.createChildRequest(body: .json(payload))
    guard case .created(let response) = output else { throw CoopAPIError.unexpectedResponse }
    return try response.body.json
  }

  public func pendingRequests() async throws -> [Components.Schemas.Request] {
    let query = Operations.ListParentRequests.Input.Query(status: .pending)
    let output = try await client.listParentRequests(query: query)
    guard case .ok(let response) = output else {
      throw CoopAPIError.unexpectedResponse
    }
    return try response.body.json.items
  }

  @discardableResult
  public func approveRequest(id: String, globally: Bool) async throws -> Components.Schemas.Request
  {
    let path = Operations.ApproveRequest.Input.Path(requestId: id)
    let payload = Operations.ApproveRequest.Input.Body.JsonPayload(
      scope: globally ? .global : .child
    )
    let output = try await client.approveRequest(path: path, body: .json(payload))
    guard case .ok(let response) = output else {
      throw CoopAPIError.unexpectedResponse
    }
    return try response.body.json
  }

  @discardableResult
  public func denyRequest(id: String, blockChannel: Bool) async throws -> Components.Schemas.Request
  {
    let path = Operations.DenyRequest.Input.Path(requestId: id)
    let payload = Operations.DenyRequest.Input.Body.JsonPayload(block: blockChannel)
    let output = try await client.denyRequest(path: path, body: .json(payload))
    guard case .ok(let response) = output else {
      throw CoopAPIError.unexpectedResponse
    }
    return try response.body.json
  }
}

public enum CoopAPIError: LocalizedError {
  case invalidCredentials
  case invalidInvitation
  case invalidPairingCode
  case invalidSession
  case searchBudgetExhausted
  case shortsDisabled
  case unexpectedResponse

  public var errorDescription: String? {
    switch self {
    case .invalidCredentials:
      "That email and password did not match."
    case .invalidInvitation:
      "That invitation is invalid, expired, or has already been used."
    case .invalidPairingCode:
      "That pairing code is invalid, expired, or has already been used."
    case .invalidSession:
      "Your parent session has expired. Sign in again."
    case .searchBudgetExhausted:
      "The family’s YouTube search budget is used up for today. Try again after it resets."
    case .shortsDisabled:
      "Shorts are not enabled for this child."
    case .unexpectedResponse:
      "The Coop server returned an unexpected response."
    }
  }
}

public struct ChildPairing: Sendable {
  public let token: String
  public let profile: Components.Schemas.ChildProfile
}

public enum ChildReaction: Sendable, Equatable {
  case like
  case dislike
}

private struct BearerAuthorizationMiddleware: ClientMiddleware {
  let token: String

  func intercept(
    _ request: HTTPRequest,
    body: HTTPBody?,
    baseURL: URL,
    operationID: String,
    next: @Sendable (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)
  ) async throws -> (HTTPResponse, HTTPBody?) {
    var request = request
    request.headerFields[.authorization] = "Bearer \(token)"
    return try await next(request, body, baseURL)
  }
}
