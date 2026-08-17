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

    let playbackItems = [
      URLQueryItem(name: "autoplay", value: "1"),
      URLQueryItem(name: "playsinline", value: "1"),
    ]
    let playbackNames = Set(playbackItems.map(\.name))
    components.queryItems =
      (components.queryItems ?? []).filter { !playbackNames.contains($0.name) }
      + playbackItems
    guard let playbackURL = components.url else { return nil }

    var request = URLRequest(url: playbackURL)
    request.setValue("https://\(bundleIdentifier.lowercased())", forHTTPHeaderField: "Referer")
    return request
  }
}

@MainActor
final class YouTubeEmbeddedPlayerSession {
  let webView: WKWebView
  fileprivate var loadedURL: URL?

  init() {
    webView = YouTubeEmbeddedPlayer.makeWebView()
  }

  func stop() {
    webView.stopLoading()
    webView.loadHTMLString("", baseURL: nil)
    loadedURL = nil
  }
}

struct YouTubeEmbeddedPlayer: UIViewRepresentable {
  let url: URL
  var session: YouTubeEmbeddedPlayerSession?

  init(url: URL, session: YouTubeEmbeddedPlayerSession? = nil) {
    self.url = url
    self.session = session
  }

  func makeCoordinator() -> Coordinator {
    Coordinator(preservesPlayback: session != nil)
  }

  func makeUIView(context: Context) -> WKWebView {
    let view = session?.webView ?? Self.makeWebView()
    load(url, in: view, coordinator: context.coordinator)
    return view
  }

  fileprivate static func makeWebView() -> WKWebView {
    let configuration = WKWebViewConfiguration()
    configuration.allowsInlineMediaPlayback = true
    configuration.mediaTypesRequiringUserActionForPlayback = []
    let view = WKWebView(frame: .zero, configuration: configuration)
    view.isOpaque = false
    view.backgroundColor = .black
    view.scrollView.isScrollEnabled = false
    return view
  }

  func updateUIView(_ view: WKWebView, context: Context) {
    load(url, in: view, coordinator: context.coordinator)
  }

  func sizeThatFits(
    _ proposal: ProposedViewSize,
    uiView _: WKWebView,
    context _: Context
  ) -> CGSize? {
    guard let width = proposal.width, let height = proposal.height else { return nil }
    return CGSize(width: width, height: height)
  }

  static func dismantleUIView(_ view: WKWebView, coordinator: Coordinator) {
    guard !coordinator.preservesPlayback else { return }
    view.stopLoading()
    view.loadHTMLString("", baseURL: nil)
    coordinator.loadedURL = nil
  }

  private func load(_ url: URL, in view: WKWebView, coordinator: Coordinator) {
    let loadedURL = session?.loadedURL ?? coordinator.loadedURL
    guard
      loadedURL != url,
      let request = YouTubeEmbedRequest.make(
        url: url,
        bundleIdentifier: Bundle.main.bundleIdentifier
      )
    else { return }
    if let session {
      session.loadedURL = url
    } else {
      coordinator.loadedURL = url
    }
    view.load(request)
  }

  final class Coordinator {
    let preservesPlayback: Bool
    var loadedURL: URL?

    init(preservesPlayback: Bool) {
      self.preservesPlayback = preservesPlayback
    }
  }
}
