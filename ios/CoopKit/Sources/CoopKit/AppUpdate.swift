import Foundation

public enum CoopApp: String, Sendable {
  case parent
  case child
}

public struct AppRelease: Codable, Equatable, Sendable {
  public let app: String
  public let title: String
  public let build: String
  public let installUrl: String
  public let installerUrl: String

  public init(
    app: String,
    title: String,
    build: String,
    installUrl: String,
    installerUrl: String
  ) {
    self.app = app
    self.title = title
    self.build = build
    self.installUrl = installUrl
    self.installerUrl = installerUrl
  }

  public var installURL: URL? { URL(string: installUrl) }
  public var installerURL: URL? { URL(string: installerUrl) }
}

public enum AppUpdate {
  public static func requiredRelease(
    serverAddress: String,
    app: CoopApp,
    currentBuild: String = Bundle.main.object(
      forInfoDictionaryKey: "CFBundleVersion") as? String ?? "0"
  ) async throws -> AppRelease? {
    let apiURL = try ServerURL.normalize(serverAddress)
    guard var components = URLComponents(url: apiURL, resolvingAgainstBaseURL: false) else {
      throw ServerURL.ValidationError.invalidURL
    }
    let apiSuffix = "/api/v1"
    guard components.path.hasSuffix(apiSuffix) else {
      throw ServerURL.ValidationError.invalidURL
    }
    components.path.removeLast(apiSuffix.count)
    components.path += "/install/releases/\(app.rawValue).json"
    components.query = nil
    guard let releaseURL = components.url else {
      throw ServerURL.ValidationError.invalidURL
    }

    let (data, response) = try await URLSession.shared.data(from: releaseURL)
    guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
      return nil
    }
    let release = try JSONDecoder().decode(AppRelease.self, from: data)
    return compareBuilds(currentBuild, release.build) == .orderedAscending ? release : nil
  }

  public static func compareBuilds(_ lhs: String, _ rhs: String) -> ComparisonResult {
    let left = lhs.split(separator: ".").compactMap { Int($0) }
    let right = rhs.split(separator: ".").compactMap { Int($0) }
    guard left.count == lhs.split(separator: ".").count,
      right.count == rhs.split(separator: ".").count
    else { return .orderedSame }

    for index in 0..<max(left.count, right.count) {
      let leftPart = index < left.count ? left[index] : 0
      let rightPart = index < right.count ? right[index] : 0
      if leftPart < rightPart { return .orderedAscending }
      if leftPart > rightPart { return .orderedDescending }
    }
    return .orderedSame
  }
}
