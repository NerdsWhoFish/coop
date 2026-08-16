import Testing

@testable import CooperWatch

@MainActor
@Test("starts at the pairing screen without a saved device")
func startsAtPairing() {
  let model = ChildAppModel()
  #expect(model.destination == .pairing)
}
