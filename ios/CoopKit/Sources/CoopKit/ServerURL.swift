import Foundation

public enum ServerURL {
  public static func normalize(_ value: String) throws -> URL {
    let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
    guard var components = URLComponents(string: trimmed) else {
      throw ValidationError.invalidURL
    }
    if components.scheme == nil {
      guard let secureComponents = URLComponents(string: "https://\(trimmed)") else {
        throw ValidationError.invalidURL
      }
      components = secureComponents
    }
    guard components.scheme == "https",
      components.host?.isEmpty == false
    else {
      throw ValidationError.httpsRequired
    }

    while components.path.hasSuffix("/") {
      components.path.removeLast()
    }
    components.path += "/api/v1"
    guard let url = components.url else {
      throw ValidationError.invalidURL
    }
    return url
  }

  public enum ValidationError: LocalizedError, Equatable {
    case invalidURL
    case httpsRequired

    public var errorDescription: String? {
      switch self {
      case .invalidURL:
        "Enter the address of your Coop server."
      case .httpsRequired:
        "Coop requires a secure HTTPS server."
      }
    }
  }
}
