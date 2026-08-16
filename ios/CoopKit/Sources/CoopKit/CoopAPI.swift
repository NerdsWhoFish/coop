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
  ) async throws -> Components.Schemas.Session {
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

  public func logIn(email: String, password: String) async throws -> Components.Schemas.Session {
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
  case invalidSession
  case unexpectedResponse

  public var errorDescription: String? {
    switch self {
    case .invalidCredentials:
      "That email and password did not match."
    case .invalidSession:
      "Your parent session has expired. Sign in again."
    case .unexpectedResponse:
      "The Coop server returned an unexpected response."
    }
  }
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
