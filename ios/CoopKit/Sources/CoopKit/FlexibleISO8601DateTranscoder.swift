import Foundation
import OpenAPIRuntime

public struct FlexibleISO8601DateTranscoder: DateTranscoder {
  private let fractional = ISO8601DateTranscoder.iso8601WithFractionalSeconds
  private let wholeSeconds = ISO8601DateTranscoder.iso8601

  public init() {}

  public func encode(_ date: Date) throws -> String {
    try fractional.encode(date)
  }

  public func decode(_ value: String) throws -> Date {
    if let date = try? fractional.decode(value) {
      return date
    }
    return try wholeSeconds.decode(value)
  }
}
