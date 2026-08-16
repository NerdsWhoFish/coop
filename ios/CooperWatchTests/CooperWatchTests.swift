import Foundation
import Testing

@testable import CooperWatch

@MainActor
@Test("starts at the pairing screen without a saved device")
func startsAtPairing() {
  let model = ChildAppModel()
  #expect(model.destination == .pairing)
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

@Test("YouTube embeds identify the app and request inline playback")
func youtubeEmbedRequest() throws {
  let request = try #require(
    YouTubeEmbedRequest.make(
      url: URL(string: "https://www.youtube-nocookie.com/embed/abc123?rel=0&playsinline=0")!,
      bundleIdentifier: "fish.NerdsWhoFish.Coop.Child"
    )
  )
  let components = try #require(
    URLComponents(url: request.url!, resolvingAgainstBaseURL: false)
  )
  let query = Dictionary(
    uniqueKeysWithValues: (components.queryItems ?? []).map { ($0.name, $0.value) })

  #expect(request.value(forHTTPHeaderField: "Referer") == "https://fish.nerdswhofish.coop.child")
  #expect(query["rel"] == "0")
  #expect(query["playsinline"] == "1")
}

@Test("YouTube embeds reject untrusted player URLs")
func youtubeEmbedRequestRejectsUnexpectedURLs() {
  let bundleIdentifier = "fish.nerdswhofish.coop.child"

  #expect(
    YouTubeEmbedRequest.make(
      url: URL(string: "http://www.youtube-nocookie.com/embed/abc123")!,
      bundleIdentifier: bundleIdentifier
    ) == nil
  )
  #expect(
    YouTubeEmbedRequest.make(
      url: URL(string: "https://www.youtube.com/embed/abc123")!,
      bundleIdentifier: bundleIdentifier
    ) == nil
  )
}
