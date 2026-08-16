import CoopKit
import Foundation
import Observation

@MainActor
@Observable
final class AppModel {
  enum Destination {
    case connecting
    case authentication(needsSetup: Bool)
    case dashboard
  }

  var destination: Destination = .connecting
  var serverAddress: String
  var requests: [Components.Schemas.Request] = []
  var isWorking = false
  var errorMessage: String?

  private var api: CoopAPI?
  private let credentials = CredentialStore()
  private let defaults = UserDefaults.standard
  private static let serverKey = "coop.server.address"

  init() {
    serverAddress = defaults.string(forKey: Self.serverKey) ?? ""
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
      if usingStoredSession, let token = credentials.loadToken() {
        let authenticatedAPI = CoopAPI(serverURL: serverURL, token: token)
        do {
          _ = try await authenticatedAPI.currentParent()
          api = authenticatedAPI
          destination = .dashboard
          await loadRequests()
          return
        } catch CoopAPIError.invalidSession {
          credentials.deleteToken()
        }
      }

      api = publicAPI
      destination = .authentication(needsSetup: needsSetup)
    }
  }

  func logIn(email: String, password: String) async {
    await perform {
      guard let api else { return }
      let session = try await api.logIn(email: email, password: password)
      try activate(session)
      await loadRequests()
    }
  }

  func setUp(familyName: String, email: String, password: String) async {
    await perform {
      guard let api else { return }
      let timezone = TimeZone.current.identifier
      let session = try await api.setUpFamily(
        familyName: familyName,
        timezone: timezone,
        email: email,
        password: password
      )
      try activate(session)
      await loadRequests()
    }
  }

  func loadRequests() async {
    guard let api else { return }
    do {
      requests = try await api.pendingRequests()
    } catch {
      errorMessage = error.localizedDescription
    }
  }

  func approve(requestID: String, globally: Bool) async throws {
    guard let api else { return }
    try await api.approveRequest(id: requestID, globally: globally)
  }

  func deny(requestID: String) async throws {
    guard let api else { return }
    try await api.denyRequest(id: requestID, blockChannel: false)
  }

  func dismiss(requestID: String) {
    requests.removeAll { $0.id == requestID }
  }

  func logOut() {
    credentials.deleteToken()
    api = nil
    requests = []
    destination = .connecting
  }

  private func activate(_ session: Components.Schemas.Session) throws {
    try credentials.saveToken(session.token)
    let serverURL = try ServerURL.normalize(serverAddress)
    api = CoopAPI(serverURL: serverURL, token: session.token)
    destination = .dashboard
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
