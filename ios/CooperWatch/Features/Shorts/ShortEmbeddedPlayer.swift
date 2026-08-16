import Foundation

enum ShortPlaybackURL {
  static func make(baseURL: String, videoID: String) -> URL? {
    guard
      var components = URLComponents(string: baseURL),
      components.scheme == "https",
      components.host == "www.youtube-nocookie.com"
    else { return nil }

    let playbackItems = [
      URLQueryItem(name: "autoplay", value: "1"),
      URLQueryItem(name: "playsinline", value: "1"),
      URLQueryItem(name: "loop", value: "1"),
      URLQueryItem(name: "playlist", value: videoID),
    ]
    let playbackNames = Set(playbackItems.map(\.name))
    components.queryItems =
      (components.queryItems ?? []).filter {
        !playbackNames.contains($0.name)
      } + playbackItems
    return components.url
  }
}
