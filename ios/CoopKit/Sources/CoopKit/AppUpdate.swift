import Foundation

public enum CoopApp: String, Sendable {
  case parent
  case child
}

/// Where over-the-air releases live. Coop hands this over rather than the apps
/// hardcoding it, so the distribution server can move without rebuilding the
/// applications that would have to carry the news.
public struct UpdateSource: Sendable, Equatable {
  public let baseURL: URL
  public let parentBundleID: String
  public let childBundleID: String

  public init?(baseURL: String, parentBundleID: String, childBundleID: String) {
    guard let url = URL(string: baseURL), url.scheme == "https", url.host() != nil,
      !parentBundleID.isEmpty, !childBundleID.isEmpty
    else { return nil }
    self.baseURL = url
    self.parentBundleID = parentBundleID
    self.childBundleID = childBundleID
  }

  public func bundleID(for app: CoopApp) -> String {
    switch app {
    case .parent: parentBundleID
    case .child: childBundleID
    }
  }
}

public struct AppReleaseNote: Codable, Equatable, Sendable {
  public let version: String
  public let build: String
  public let notes: String?

  public init(version: String, build: String, notes: String? = nil) {
    self.version = version
    self.build = build
    self.notes = notes
  }
}

public struct AppRelease: Codable, Equatable, Sendable {
  public let bundleId: String
  public let name: String
  public let version: String
  public let build: String
  public let buildId: String
  public let installPageUrl: String
  public let expired: Bool
  public let changelog: [AppReleaseNote]

  public init(
    bundleId: String,
    name: String,
    version: String,
    build: String,
    buildId: String,
    installPageUrl: String,
    expired: Bool = false,
    changelog: [AppReleaseNote] = []
  ) {
    self.bundleId = bundleId
    self.name = name
    self.version = version
    self.build = build
    self.buildId = buildId
    self.installPageUrl = installPageUrl
    self.expired = expired
    self.changelog = changelog
  }

  /// The page to send a person to. Deliberately not an `itms-services://` URL:
  /// only Safari handles that scheme, and an embedded browser swallows it with
  /// no dialog, which reads as the build being broken.
  public var installPageURL: URL? { URL(string: installPageUrl) }
}

public enum AppUpdate {
  /// The published release, when it is newer than what is running. The server's
  /// own update flag is ignored because it is string inequality against the
  /// newest upload, so a re-published older archive would read as an update.
  public static func requiredRelease(
    source: UpdateSource,
    app: CoopApp,
    currentBuild: String = Bundle.main.object(
      forInfoDictionaryKey: "CFBundleVersion") as? String ?? "0"
  ) async throws -> AppRelease? {
    let endpoint = source.baseURL
      .appending(path: "api/v1/apps/\(source.bundleID(for: app))/latest")
    guard var components = URLComponents(url: endpoint, resolvingAgainstBaseURL: false) else {
      throw ServerURL.ValidationError.invalidURL
    }
    components.queryItems = [URLQueryItem(name: "build", value: currentBuild)]
    guard let releaseURL = components.url else {
      throw ServerURL.ValidationError.invalidURL
    }

    let (data, response) = try await URLSession.shared.data(from: releaseURL)
    guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
      return nil
    }
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    let release = try decoder.decode(AppRelease.self, from: data)
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
