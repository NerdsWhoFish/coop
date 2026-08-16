import Foundation
import Testing

@testable import CoopKit

@Suite("Flexible ISO 8601 date transcoder")
struct FlexibleISO8601DateTranscoderTests {
  private let transcoder = FlexibleISO8601DateTranscoder()

  @Test("decodes whole seconds")
  func decodesWholeSeconds() throws {
    let date = try transcoder.decode("2026-08-16T16:28:34Z")
    let components = Calendar(identifier: .gregorian).dateComponents(
      in: TimeZone(secondsFromGMT: 0)!,
      from: date
    )

    #expect(components.year == 2026)
    #expect(components.month == 8)
    #expect(components.day == 16)
    #expect(components.hour == 16)
    #expect(components.minute == 28)
    #expect(components.second == 34)
  }

  @Test("decodes Go fractional seconds")
  func decodesFractionalSeconds() throws {
    let wholeSeconds = try transcoder.decode("2026-08-16T16:28:34Z")
    let fractional = try transcoder.decode("2026-08-16T16:28:34.891535714Z")

    #expect(abs(fractional.timeIntervalSince(wholeSeconds) - 0.891535714) < 0.001)
  }

  @Test("rejects malformed timestamps")
  func rejectsMalformedTimestamp() {
    #expect(throws: DecodingError.self) {
      try transcoder.decode("2026-08-16 16:28:34")
    }
  }
}
