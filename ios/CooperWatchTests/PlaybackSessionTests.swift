import Foundation
import Testing
import UIKit
import WebKit

@testable import CooperWatch

@MainActor
@Test("Background suspension retains the player and playback position across repeated returns")
func playbackSurvivesBackgroundSuspension() async throws {
  let session = try visibleSession()
  let webView = session.webView
  defer { webView.removeFromSuperview() }
  webView.loadHTMLString(mediaFixture(), baseURL: nil)
  try await waitForJavaScript("document.querySelector('audio')?.readyState >= 2", in: webView)
  _ = try await webView.evaluateJavaScript("document.querySelector('audio').currentTime = 12")
  try await waitForJavaScript("document.querySelector('audio').currentTime >= 12", in: webView)
  _ = try await webView.evaluateJavaScript("document.querySelector('audio').play(); true")
  try await waitForJavaScript("document.querySelector('audio').currentTime > 12", in: webView)

  for _ in 0..<3 {
    session.setPlaybackActive(false)
    try await waitForJavaScript("document.querySelector('audio').paused", in: webView)
    let pausedAt = try #require(
      try await webView.evaluateJavaScript("document.querySelector('audio').currentTime") as? Double
    )
    #expect(pausedAt >= 12)
    // Suspension must also prevent the embed itself from restarting playback.
    _ = try await webView.evaluateJavaScript(
      "document.querySelector('audio').play().catch(() => {}); true"
    )
    try await Task.sleep(for: .milliseconds(200))
    let stillPausedAt = try #require(
      try await webView.evaluateJavaScript("document.querySelector('audio').currentTime") as? Double
    )
    #expect(abs(stillPausedAt - pausedAt) < 0.1)

    session.setPlaybackActive(true)
    _ = try await webView.evaluateJavaScript("document.querySelector('audio').play(); true")
    try await waitForJavaScript(
      "document.querySelector('audio').currentTime > \(pausedAt)", in: webView
    )
    #expect(try await webView.evaluateJavaScript("window.playerIdentity") as? String == "original")
  }

  // Leaving the video or a parent blocking it still destroys the player.
  session.stop()
  try await waitForJavaScript("document.querySelector('audio') === null", in: webView)
  #expect(
    try await webView.evaluateJavaScript("typeof window.playerIdentity") as? String == "undefined"
  )
}

@MainActor
@Test("Returning to the app preserves a video the child paused manually")
func backgroundSuspensionPreservesManualPause() async throws {
  let session = try visibleSession()
  let webView = session.webView
  defer { webView.removeFromSuperview() }
  webView.loadHTMLString(mediaFixture(), baseURL: nil)
  try await waitForJavaScript("document.querySelector('audio')?.readyState >= 2", in: webView)
  _ = try await webView.evaluateJavaScript(
    "document.querySelector('audio').pause(); document.querySelector('audio').currentTime = 8"
  )
  try await waitForJavaScript("document.querySelector('audio').currentTime >= 8", in: webView)

  session.setPlaybackActive(false)
  session.setPlaybackActive(true)
  try await Task.sleep(for: .milliseconds(200))
  #expect(
    try await webView.evaluateJavaScript("document.querySelector('audio').paused") as? Bool == true
  )
  let position = try #require(
    try await webView.evaluateJavaScript("document.querySelector('audio').currentTime") as? Double
  )
  #expect(abs(position - 8) < 0.1)
  session.stop()
}

@MainActor
private func visibleSession() throws -> YouTubeEmbeddedPlayerSession {
  // Keep media visible so the fixture exercises the same lifecycle as the app.
  let window = try #require(
    UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
      .flatMap(\.windows).first { $0.isKeyWindow }
  )
  let session = YouTubeEmbeddedPlayerSession()
  session.webView.frame = window.bounds
  window.addSubview(session.webView)
  return session
}

@MainActor
private func waitForJavaScript(_ condition: String, in webView: WKWebView) async throws {
  for _ in 0..<100 {
    if try await webView.evaluateJavaScript(condition) as? Bool == true { return }
    try await Task.sleep(for: .milliseconds(100))
  }
  struct PlayerConditionTimedOut: Error {
    let condition: String
  }
  throw PlayerConditionTimedOut(condition: condition)
}

private func mediaFixture() -> String {
  // A seekable silent PCM WAV keeps these WebKit tests independent of YouTube,
  // network access, expiring media URLs, and third-party playback policies.
  let sampleCount = 8_000 * 30
  var wav = Data()
  func text(_ value: String) { wav.append(contentsOf: value.utf8) }
  func number<T: FixedWidthInteger>(_ value: T) {
    var littleEndian = value.littleEndian
    withUnsafeBytes(of: &littleEndian) { wav.append(contentsOf: $0) }
  }
  text("RIFF")
  number(UInt32(36 + sampleCount * 2))
  text("WAVEfmt ")
  number(UInt32(16))
  number(UInt16(1))
  number(UInt16(1))
  number(UInt32(8_000))
  number(UInt32(16_000))
  number(UInt16(2))
  number(UInt16(16))
  text("data")
  number(UInt32(sampleCount * 2))
  wav.append(Data(count: sampleCount * 2))
  return """
    <html><body>
    <audio autoplay preload="auto" src="data:audio/wav;base64,\(wav.base64EncodedString())"></audio>
    <script>window.playerIdentity = 'original';</script>
    </body></html>
    """
}
