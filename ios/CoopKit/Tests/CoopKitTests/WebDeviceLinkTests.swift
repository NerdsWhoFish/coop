import Foundation
import Testing

@testable import CoopKit

@Suite("Web device link")
struct WebDeviceLinkTests {
  @Test("parses a valid two-secret handoff")
  func parsesValidPayload() throws {
    let id = UUID().uuidString
    let payload = try WebDeviceLinkPayload(
      scannedValue: "coop://link?server=https%3A%2F%2Fcoop.example&id=\(id)&approval=secret"
    )

    #expect(payload.serverURL == URL(string: "https://coop.example"))
    #expect(payload.id == id)
    #expect(payload.approvalToken == "secret")
  }

  @Test("rejects insecure and malformed payloads", arguments: [
    "https://coop.example/link",
    "coop://link?server=http%3A%2F%2Fcoop.example&id=not-a-uuid&approval=secret",
    "coop://link?server=https%3A%2F%2Fcoop.example&id=not-a-uuid&approval=secret",
  ])
  func rejectsInvalidPayload(value: String) {
    #expect(throws: WebDeviceLinkError.self) {
      try WebDeviceLinkPayload(scannedValue: value)
    }
  }
}
