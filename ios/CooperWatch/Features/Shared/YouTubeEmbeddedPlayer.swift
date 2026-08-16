import SwiftUI
import WebKit

enum YouTubeEmbedRequest {
  static func make(url: URL, bundleIdentifier: String?) -> URLRequest? {
    guard
      var components = URLComponents(url: url, resolvingAgainstBaseURL: false),
      components.scheme == "https",
      components.host == "www.youtube-nocookie.com",
      let bundleIdentifier,
      !bundleIdentifier.isEmpty
    else { return nil }

    components.queryItems =
      (components.queryItems ?? []).filter { $0.name != "playsinline" }
      + [URLQueryItem(name: "playsinline", value: "1")]
    guard let playbackURL = components.url else { return nil }

    var request = URLRequest(url: playbackURL)
    request.setValue("https://\(bundleIdentifier.lowercased())", forHTTPHeaderField: "Referer")
    return request
  }
}

struct YouTubeEmbeddedPlayer: UIViewRepresentable {
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
    guard
      coordinator.loadedURL != url,
      let request = YouTubeEmbedRequest.make(
        url: url,
        bundleIdentifier: Bundle.main.bundleIdentifier
      )
    else { return }
    coordinator.loadedURL = url
    view.load(request)
  }

  final class Coordinator {
    var loadedURL: URL?
  }
}
