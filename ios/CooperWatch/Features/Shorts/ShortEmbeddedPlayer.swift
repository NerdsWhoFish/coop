import SwiftUI
import WebKit

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

struct ShortEmbeddedPlayer: UIViewRepresentable {
  let url: URL

  func makeCoordinator() -> Coordinator {
    Coordinator()
  }

  func makeUIView(context: Context) -> WKWebView {
    let configuration = WKWebViewConfiguration()
    configuration.allowsInlineMediaPlayback = true
    configuration.mediaTypesRequiringUserActionForPlayback = []
    let view = WKWebView(frame: .zero, configuration: configuration)
    view.isOpaque = false
    view.backgroundColor = .black
    view.scrollView.isScrollEnabled = false
    load(url, in: view, coordinator: context.coordinator)
    return view
  }

  func updateUIView(_ view: WKWebView, context: Context) {
    load(url, in: view, coordinator: context.coordinator)
  }

  static func dismantleUIView(_ view: WKWebView, coordinator: Coordinator) {
    view.stopLoading()
    view.loadHTMLString("", baseURL: nil)
    coordinator.loadedURL = nil
  }

  private func load(_ url: URL, in view: WKWebView, coordinator: Coordinator) {
    guard coordinator.loadedURL != url else { return }
    coordinator.loadedURL = url
    view.load(URLRequest(url: url))
  }

  final class Coordinator {
    var loadedURL: URL?
  }
}
