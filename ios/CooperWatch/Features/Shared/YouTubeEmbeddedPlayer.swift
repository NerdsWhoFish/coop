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
  var onReady: () -> Void

  init(
    url: URL,
    session: YouTubeEmbeddedPlayerSession? = nil,
    onReady: @escaping () -> Void = {}
  ) {
    self.url = url
    self.session = session
    self.onReady = onReady
  }

  func makeCoordinator() -> Coordinator {
    Coordinator(preservesPlayback: session != nil, onReady: onReady)
  }

  func makeUIView(context: Context) -> WKWebView {
    let view = session?.webView ?? Self.makeWebView()
    context.coordinator.attach(to: view)
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
    context.coordinator.onReady = onReady
    context.coordinator.attach(to: view)
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
    coordinator.detach(from: view)
    guard !coordinator.preservesPlayback else { return }
    view.stopLoading()
    view.loadHTMLString("", baseURL: nil)
    coordinator.loadedURL = nil
  }

  private func load(_ url: URL, in view: WKWebView, coordinator: Coordinator) {
    coordinator.observeReadiness(of: url, in: view)
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

  @MainActor
  final class Coordinator: NSObject, WKNavigationDelegate, WKScriptMessageHandler {
    private static let messageName = "coopPlayerReady"
    // Navigation completion only means YouTube's document loaded; the player can still be black
    // while its media initializes, so keep the placeholder until the video has usable data.
    private static let readinessScript = """
      (() => {
        clearInterval(window.__coopPlayerReadyInterval);
        const notifyWhenReady = () => {
          const video = document.querySelector('video');
          if (!video || video.readyState < 2) return;
          clearInterval(window.__coopPlayerReadyInterval);
          window.webkit.messageHandlers.coopPlayerReady.postMessage(true);
        };
        window.__coopPlayerReadyInterval = setInterval(notifyWhenReady, 250);
        notifyWhenReady();
      })();
      """

    let preservesPlayback: Bool
    var loadedURL: URL?
    var onReady: () -> Void
    private weak var attachedView: WKWebView?
    private var observedURL: URL?
    private var reportedReady = false

    init(preservesPlayback: Bool, onReady: @escaping () -> Void = {}) {
      self.preservesPlayback = preservesPlayback
      self.onReady = onReady
    }

    func attach(to view: WKWebView) {
      guard attachedView !== view else { return }
      let controller = view.configuration.userContentController
      controller.removeScriptMessageHandler(forName: Self.messageName)
      controller.add(self, name: Self.messageName)
      view.navigationDelegate = self
      attachedView = view
    }

    func observeReadiness(of url: URL, in view: WKWebView) {
      if observedURL != url {
        stopObservingReadiness()
        observedURL = url
        reportedReady = false
      }
      guard !reportedReady else { return }
      view.evaluateJavaScript(Self.readinessScript)
      NSObject.cancelPreviousPerformRequests(
        withTarget: self,
        selector: #selector(revealPlayerAfterTimeout),
        object: nil
      )
      perform(#selector(revealPlayerAfterTimeout), with: nil, afterDelay: 45)
    }

    func detach(from view: WKWebView) {
      stopObservingReadiness()
      view.configuration.userContentController.removeScriptMessageHandler(
        forName: Self.messageName
      )
      view.navigationDelegate = nil
      attachedView = nil
    }

    func webView(_ webView: WKWebView, didFinish _: WKNavigation!) {
      guard !reportedReady else { return }
      webView.evaluateJavaScript(Self.readinessScript)
    }

    func userContentController(
      _: WKUserContentController,
      didReceive message: WKScriptMessage
    ) {
      guard message.name == Self.messageName else { return }
      markReady()
    }

    private func stopObservingReadiness() {
      NSObject.cancelPreviousPerformRequests(
        withTarget: self,
        selector: #selector(revealPlayerAfterTimeout),
        object: nil
      )
    }

    @objc private func revealPlayerAfterTimeout() {
      markReady()
    }

    private func markReady() {
      guard !reportedReady else { return }
      reportedReady = true
      stopObservingReadiness()
      onReady()
    }
  }
}
