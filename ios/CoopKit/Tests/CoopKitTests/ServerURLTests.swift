import Foundation
import Testing

@testable import CoopKit

@Suite("Server URL")
struct ServerURLTests {
  @Test("adds HTTPS and the API base path")
  func normalizesHost() throws {
    let result = try ServerURL.normalize("coop.example")
    #expect(result.absoluteString == "https://coop.example/api/v1")
  }

  @Test("removes trailing slashes")
  func removesTrailingSlash() throws {
    let result = try ServerURL.normalize("https://coop.example/")
    #expect(result.absoluteString == "https://coop.example/api/v1")
  }

  @Test("rejects insecure instances")
  func rejectsHTTP() {
    #expect(throws: ServerURL.ValidationError.httpsRequired) {
      try ServerURL.normalize("http://coop.example")
    }
  }
}
