import CoopKit
import Foundation
import Testing

@testable import CooperWatch

@MainActor
@Test("starts at the splash screen while saved state is restored")
func startsAtSplash() {
  let model = ChildAppModel()
  #expect(model.destination == .launching)
}

@MainActor
@Test("watch-next keeps ranked regular videos and removes the current video")
func watchNextFiltersCurrentVideoAndShorts() async {
  let model = ChildAppModel()
  model.feed = [
    video(id: "current"),
    video(id: "first"),
    video(id: "short", isShort: true),
    video(id: "second"),
  ]

  let next = await model.watchNext(excluding: "current")
  #expect(next.map(\.id) == ["first", "second"])
}

@Test("Short playback enables inline autoplay and YouTube's loop contract")
func shortPlaybackURL() throws {
  let url = try #require(
    ShortPlaybackURL.make(
      baseURL: "https://www.youtube-nocookie.com/embed/abc123?rel=0&autoplay=0",
      videoID: "abc123"
    )
  )
  let components = try #require(URLComponents(url: url, resolvingAgainstBaseURL: false))
  let query = Dictionary(
    uniqueKeysWithValues: (components.queryItems ?? []).map { ($0.name, $0.value) })

  #expect(query["rel"] == "0")
  #expect(query["autoplay"] == "1")
  #expect(query["playsinline"] == "1")
  #expect(query["loop"] == "1")
  #expect(query["playlist"] == "abc123")
}

@Test("Short playback rejects a non-privacy-enhanced player host")
func shortPlaybackRejectsUnexpectedHost() {
  let url = ShortPlaybackURL.make(
    baseURL: "https://www.youtube.com/embed/abc123",
    videoID: "abc123"
  )
  #expect(url == nil)
}

@Test("YouTube embeds always autoplay inline")
func youtubeEmbedPlaybackURL() throws {
  let playbackURL = try #require(
    YouTubeEmbedRequest.playbackURL(
      for: URL(
        string: "https://www.youtube-nocookie.com/embed/abc123?rel=0&autoplay=0&playsinline=0"
      )!
    )
  )
  let components = try #require(
    URLComponents(url: playbackURL, resolvingAgainstBaseURL: false)
  )
  let query = Dictionary(
    uniqueKeysWithValues: (components.queryItems ?? []).map { ($0.name, $0.value) })

  #expect(query["rel"] == "0")
  #expect(query["autoplay"] == "1")
  #expect(query["playsinline"] == "1")
}

@Test("YouTube embeds reject untrusted player URLs")
func youtubeEmbedRejectsUnexpectedURLs() {
  #expect(
    YouTubeEmbedRequest.playbackURL(
      for: URL(string: "http://www.youtube-nocookie.com/embed/abc123")!
    ) == nil
  )
  #expect(
    YouTubeEmbedRequest.playbackURL(
      for: URL(string: "https://www.youtube.com/embed/abc123")!
    ) == nil
  )
}

@Test("The embed wrapper frames the player for a real referrer")
func youtubeEmbedWrapper() {
  let html = YouTubeEmbedRequest.wrapperHTML(
    for: URL(string: "https://www.youtube-nocookie.com/embed/abc123?autoplay=1")!
  )
  #expect(html.contains("<iframe src=\"https://www.youtube-nocookie.com/embed/abc123?autoplay=1\""))
  #expect(html.contains("allow=\"autoplay"))
}

@MainActor
@Test("Player link routing recognizes only real video destinations")
func playerLinkVideoIDs() {
  func id(_ raw: String) -> String? {
    PlayerLinkRouter.videoID(from: URL(string: raw)!)
  }
  #expect(id("https://www.youtube.com/watch?v=dQw4w9WgXcQ") == "dQw4w9WgXcQ")
  #expect(id("https://m.youtube.com/watch?v=abc&t=4s") == "abc")
  #expect(id("https://youtu.be/xyz789") == "xyz789")
  #expect(id("https://www.youtube.com/shorts/short123") == "short123")
  #expect(id("https://www.youtube-nocookie.com/embed/abc123") == nil)
  #expect(id("https://example.com/watch?v=abc") == nil)
  #expect(id("https://www.youtube.com/@somechannel") == nil)
}

private func video(id: String, isShort: Bool = false) -> Components.Schemas.Video {
  Components.Schemas.Video(
    id: id,
    channelId: "approved-channel",
    title: id,
    durationSeconds: 120,
    isShort: isShort
  )
}
