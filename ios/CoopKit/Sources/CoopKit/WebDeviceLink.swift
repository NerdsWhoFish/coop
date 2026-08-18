import Foundation

public struct WebDeviceLinkPayload: Equatable, Sendable {
  public let serverURL: URL
  public let id: String
  public let approvalToken: String

  public init(scannedValue: String) throws {
    guard
      let components = URLComponents(string: scannedValue),
      components.scheme == "coop",
      components.host == "link",
      let server = components.queryItems?.first(where: { $0.name == "server" })?.value,
      let serverURL = URL(string: server),
      serverURL.scheme == "https",
      let id = components.queryItems?.first(where: { $0.name == "id" })?.value,
      UUID(uuidString: id) != nil,
      let approvalToken = components.queryItems?.first(where: { $0.name == "approval" })?.value,
      !approvalToken.isEmpty
    else {
      throw WebDeviceLinkError.invalidCode
    }
    self.serverURL = serverURL
    self.id = id
    self.approvalToken = approvalToken
  }
}

public enum WebDeviceLinkError: LocalizedError {
  case invalidCode
  case differentServer

  public var errorDescription: String? {
    switch self {
    case .invalidCode:
      "That isn’t a Coop computer-link code."
    case .differentServer:
      "That computer is connected to a different Coop server."
    }
  }
}
