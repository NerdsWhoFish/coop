import CoopKit
import SwiftUI

/// Every installed Coop app in the family and the build it last reported.
/// A device that reports nothing is running a build too old to say, which is
/// exactly the device an update is waiting on.
struct AppVersionsView: View {
  @Bindable var model: AppModel
  @State private var devices: [Components.Schemas.FamilyDevice] = []
  @State private var loaded = false

  var body: some View {
    List {
      if !outdated.isEmpty {
        Section {
          Label(
            "\(outdated.count) \(outdated.count == 1 ? "device is" : "devices are") behind",
            systemImage: "exclamationmark.triangle.fill"
          )
          .foregroundStyle(CoopTheme.orange)
        } footer: {
          Text(
            "A device updates when someone opens its app and confirms the install. "
              + "iOS cannot install it for them."
          )
        }
        .listRowBackground(CoopTheme.surface)
      }

      section("Parents", devices: devices.filter { $0.audience == .parent })
      section("Children", devices: devices.filter { $0.audience == .child })
    }
    .scrollContentBackground(.hidden)
    .overlay {
      if loaded && devices.isEmpty {
        ContentUnavailableView(
          "Nothing signed in",
          systemImage: "iphone.slash",
          description: Text("Devices appear here once they connect to this server.")
        )
        .allowsHitTesting(false)
      }
    }
    .refreshable { await load() }
    .navigationTitle("App versions")
    .navigationBarTitleDisplayMode(.inline)
    .task { await load() }
    .coopBackground()
  }

  @ViewBuilder
  private func section(_ title: String, devices: [Components.Schemas.FamilyDevice]) -> some View {
    if !devices.isEmpty {
      Section(title) {
        ForEach(devices, id: \.id) { device in
          row(device)
        }
      }
    }
  }

  private func row(_ device: Components.Schemas.FamilyDevice) -> some View {
    VStack(alignment: .leading, spacing: 5) {
      HStack(spacing: 8) {
        Text(device.name ?? device.owner).font(.headline)
        if device.current {
          Text("THIS DEVICE")
            .font(.caption2.weight(.black)).tracking(1.1)
            .foregroundStyle(CoopTheme.cyan)
        }
      }
      if device.name != nil {
        Text(device.owner)
          .font(.subheadline)
          .foregroundStyle(CoopTheme.foreground.opacity(0.72))
      }
      HStack(spacing: 8) {
        Text(versionLabel(device))
          .font(.caption.monospacedDigit().weight(.semibold))
          .foregroundStyle(isCurrent(device) ? CoopTheme.green : CoopTheme.orange)
        Text(lastSeen(device))
          .font(.caption)
          .foregroundStyle(CoopTheme.foreground.opacity(0.62))
      }
    }
    .listRowBackground(CoopTheme.surface)
  }

  /// The newest build anyone reports is treated as current. It is the only
  /// build known to exist without asking the distribution server, and a device
  /// reporting nothing at all is behind by definition.
  private var newestBuild: String? {
    devices.compactMap(\.appBuild).max {
      AppUpdate.compareBuilds($0, $1) == .orderedAscending
    }
  }

  private var outdated: [Components.Schemas.FamilyDevice] {
    devices.filter { !isCurrent($0) }
  }

  private func isCurrent(_ device: Components.Schemas.FamilyDevice) -> Bool {
    guard let build = device.appBuild, let newest = newestBuild else { return false }
    return AppUpdate.compareBuilds(build, newest) != .orderedAscending
  }

  private func versionLabel(_ device: Components.Schemas.FamilyDevice) -> String {
    guard let build = device.appBuild else { return "Version unknown" }
    guard let version = device.appVersion else { return "Build \(build)" }
    return "\(version) (\(build))"
  }

  private func lastSeen(_ device: Components.Schemas.FamilyDevice) -> String {
    guard let lastSeenAt = device.lastSeenAt else { return "• Never connected" }
    return "• \(lastSeenAt.formatted(.relative(presentation: .named)))"
  }

  private func load() async {
    do {
      devices = try await model.familyDevices()
    } catch {
      model.errorMessage = error.localizedDescription
    }
    loaded = true
  }
}
