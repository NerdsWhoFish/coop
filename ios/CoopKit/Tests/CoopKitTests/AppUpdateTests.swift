import CoopKit
import Foundation
import Testing

struct AppUpdateTests {
  @Test("compares numeric build components")
  func comparesBuilds() {
    #expect(AppUpdate.compareBuilds("12", "13") == .orderedAscending)
    #expect(AppUpdate.compareBuilds("13", "13") == .orderedSame)
    #expect(AppUpdate.compareBuilds("14", "13") == .orderedDescending)
    #expect(AppUpdate.compareBuilds("1.9", "1.10") == .orderedAscending)
    #expect(AppUpdate.compareBuilds("1.2", "1.2.0") == .orderedSame)
  }

  @Test("fails open for malformed build metadata")
  func malformedBuildsAreEqual() {
    #expect(AppUpdate.compareBuilds("local", "13") == .orderedSame)
    #expect(AppUpdate.compareBuilds("12", "latest") == .orderedSame)
  }
}
