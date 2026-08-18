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

/// Reports playable media once per load. Owned by the web view's lifetime,
/// never a SwiftUI layout's: transient representables racing attach/detach
/// across rotations is what used to strand the placeholder over playback.
@MainActor
final class PlayerReadinessObserver: NSObject, WKScriptMessageHandler {
  private static let messageName = "coopPlayerReady"

  // Ready means the video element has usable data, not that the document
  // loaded. A user script re-arms itself on every navigation automatically.
  private static let script = WKUserScript(
    source: """
      (() => {
        clearInterval(window.__coopPlayerReadyInterval);
        let attempts = 0;
        const notifyWhenReady = () => {
          if (attempts++ > 480) { clearInterval(window.__coopPlayerReadyInterval); return; }
          const video = document.querySelector('video');
          if (!video || video.readyState < 2) return;
          clearInterval(window.__coopPlayerReadyInterval);
          window.webkit.messageHandlers.coopPlayerReady.postMessage(true);
        };
        window.__coopPlayerReadyInterval = setInterval(notifyWhenReady, 250);
        notifyWhenReady();
      })();
      """,
    injectionTime: .atDocumentEnd,
    forMainFrameOnly: true
  )

  // A kid staring at a spinner experiences the old 45 second failsafe as
  // broken; reveal whatever the player has after a bounded wait instead.
  private static let revealTimeout: Duration = .seconds(10)

  private var onReady: (() -> Void)?
  private var reportedReady = false
  private var timeout: Task<Void, Never>?

  /// Installs the script and message plumbing once for the web view's life.
  static func install(on webView: WKWebView) -> PlayerReadinessObserver {
    let observer = PlayerReadinessObserver()
    let controller = webView.configuration.userContentController
    controller.addUserScript(script)
    controller.add(WeakMessageHandler(target: observer), name: messageName)
    return observer
  }

  /// Arms for the current load; repeat calls only swap the callback. Fires
  /// immediately if already ready, so a relaid-out player skips the placeholder.
  func expectReady(_ onReady: @escaping () -> Void) {
    self.onReady = onReady
    if reportedReady {
      onReady()
      return
    }
    scheduleTimeout()
  }

  /// Forgets the current load so the next one shows a placeholder again.
  func reset() {
    reportedReady = false
    onReady = nil
    timeout?.cancel()
    timeout = nil
  }

  func userContentController(
    _: WKUserContentController,
    didReceive message: WKScriptMessage
  ) {
    guard message.name == Self.messageName else { return }
    markReady()
  }

  private func scheduleTimeout() {
    timeout?.cancel()
    timeout = Task { [weak self] in
      try? await Task.sleep(for: Self.revealTimeout)
      guard !Task.isCancelled else { return }
      self?.markReady()
    }
  }

  private func markReady() {
    guard !reportedReady else { return }
    reportedReady = true
    timeout?.cancel()
    timeout = nil
    onReady?()
  }
}

// WKUserContentController retains its message handlers, so registering the
// observer directly would cycle through the web view's configuration.
private final class WeakMessageHandler: NSObject, WKScriptMessageHandler {
  weak var target: WKScriptMessageHandler?

  init(target: WKScriptMessageHandler) {
    self.target = target
  }

  func userContentController(
    _ controller: WKUserContentController,
    didReceive message: WKScriptMessage
  ) {
    target?.userContentController(controller, didReceive: message)
  }
}

@MainActor
final class YouTubeEmbeddedPlayerSession {
  let webView: WKWebView
  let readiness: PlayerReadinessObserver
  fileprivate var loadedURL: URL?

  init() {
    webView = YouTubeEmbeddedPlayer.makeWebView()
    readiness = PlayerReadinessObserver.install(on: webView)
  }

  func stop() {
    webView.stopLoading()
    webView.loadHTMLString("", baseURL: nil)
    loadedURL = nil
    readiness.reset()
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
    Coordinator(preservesPlayback: session != nil)
  }

  func makeUIView(context: Context) -> WKWebView {
    let view: WKWebView
    if let session {
      view = session.webView
      context.coordinator.readiness = session.readiness
    } else {
      view = Self.makeWebView()
      context.coordinator.readiness = PlayerReadinessObserver.install(on: view)
    }
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
    coordinator.readiness?.expectReady(onReady)
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
      session.readiness.reset()
      session.readiness.expectReady(onReady)
    } else {
      coordinator.loadedURL = url
    }
    view.load(request)
  }

  @MainActor
  final class Coordinator {
    let preservesPlayback: Bool
    var loadedURL: URL?
    var readiness: PlayerReadinessObserver?

    init(preservesPlayback: Bool) {
      self.preservesPlayback = preservesPlayback
    }
  }
}
