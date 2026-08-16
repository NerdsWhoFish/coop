import Foundation
import Testing

@testable import CooperTheCop

@Suite("App shell")
struct CooperTheCopTests {
  @Test("starts at server connection")
  @MainActor
  func startsDisconnectedWithoutSavedSettings() {
    let defaults = UserDefaults.standard
    let previous = defaults.string(forKey: "coop.server.address")
    defaults.removeObject(forKey: "coop.server.address")
    defer { defaults.set(previous, forKey: "coop.server.address") }

    let model = AppModel()
    if case .connecting = model.destination {
      return
    }
    Issue.record("AppModel did not start at server connection")
  }

  @Test("channel preference labels cover every stored weight")
  func channelPreferenceLabels() {
    #expect(ChannelPreference.allCases.map(\.rawValue) == [-2, -1, 0, 1, 2])
    #expect(
      ChannelPreference.allCases.map(\.label) == [
        "Much less", "Less", "Balanced", "More", "Much more",
      ])
  }
}
