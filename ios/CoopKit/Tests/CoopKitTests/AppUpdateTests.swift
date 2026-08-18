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

  /// Comparison is positional over dot-separated integers, so a device on build
  /// 22 reads "1.8.0" as older than itself and never updates again. Builds are
  /// encoded as major * 10000 + minor * 100 + patch to stay ahead and ordered.
  @Test("a marketing version used as a build number strands installed devices")
  func marketingVersionIsNotABuildNumber() {
    #expect(AppUpdate.compareBuilds("22", "1.8.0") == .orderedDescending)
    #expect(AppUpdate.compareBuilds("22", "10800") == .orderedAscending)
    #expect(AppUpdate.compareBuilds("14", "10800") == .orderedAscending)
    #expect(AppUpdate.compareBuilds("10800", "10801") == .orderedAscending)
    #expect(AppUpdate.compareBuilds("10800", "20000") == .orderedAscending)
  }

  @Test("an update source needs a usable HTTPS origin and both bundles")
  func rejectsUnusableSources() {
    #expect(
      UpdateSource(
        baseURL: "http://fledge.example", parentBundleID: "a", childBundleID: "b") == nil)
    #expect(
      UpdateSource(baseURL: "fledge.example", parentBundleID: "a", childBundleID: "b") == nil)
    #expect(
      UpdateSource(baseURL: "https://fledge.example", parentBundleID: "", childBundleID: "b")
        == nil)
    #expect(
      UpdateSource(baseURL: "https://fledge.example", parentBundleID: "a", childBundleID: "")
        == nil)
  }

  @Test("an update source names the bundle for each application")
  func namesBundlePerApp() throws {
    let source = try #require(
      UpdateSource(
        baseURL: "https://fledge.example",
        parentBundleID: "fish.nerdswhofish.coop.parent",
        childBundleID: "fish.nerdswhofish.coop.child"
      ))
    #expect(source.bundleID(for: .parent) == "fish.nerdswhofish.coop.parent")
    #expect(source.bundleID(for: .child) == "fish.nerdswhofish.coop.child")
  }

  @Test("decodes the release shape the distribution server publishes")
  func decodesRelease() throws {
    let json = """
      {
        "bundle_id": "fish.nerdswhofish.coop.parent",
        "name": "Cooper The Cop",
        "version": "1.8.0",
        "build": "10800",
        "build_id": "570fdeca6768",
        "install_page_url": "https://fledge.example/a/fish.nerdswhofish.coop.parent",
        "update_available": true,
        "expired": false,
        "changelog": [
          {"version": "1.8.0", "build": "10800", "notes": "Fledge migration."}
        ]
      }
      """
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    let release = try decoder.decode(AppRelease.self, from: Data(json.utf8))

    #expect(release.build == "10800")
    #expect(release.version == "1.8.0")
    #expect(release.buildId == "570fdeca6768")
    #expect(release.expired == false)
    #expect(release.changelog.first?.notes == "Fledge migration.")
    #expect(
      release.installPageURL
        == URL(string: "https://fledge.example/a/fish.nerdswhofish.coop.parent"))
  }
}
