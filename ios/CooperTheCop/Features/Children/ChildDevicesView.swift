import CoopKit
import SwiftUI

struct ChildDevicesView: View {
  let child: Components.Schemas.Child
  @Bindable var model: AppModel
  @State private var devices: [Components.Schemas.Device] = []
  @State private var revoking: Components.Schemas.Device?
  @State private var updatingDeviceIDs: Set<String> = []

  var body: some View {
    List(devices, id: \.id) { device in
      VStack(alignment: .leading, spacing: 12) {
        VStack(alignment: .leading, spacing: 4) {
          Text(device.name).font(.headline)
          Text(lastSeen(device))
            .font(.caption).foregroundStyle(CoopTheme.foreground.opacity(0.62))
        }
        Toggle(
          "Allow re-pairing",
          isOn: Binding(
            get: { device.allowSelfUnpair },
            set: { update(device, allowSelfUnpair: $0) }
          )
        )
        .tint(CoopTheme.cyan)
        .disabled(updatingDeviceIDs.contains(device.id))
      }
      .swipeActions {
        Button("Revoke", role: .destructive) { revoking = device }
      }
      .listRowBackground(CoopTheme.surface)
    }
    .scrollContentBackground(.hidden)
    .overlay {
      if devices.isEmpty {
        ContentUnavailableView(
          "No paired devices",
          systemImage: "iphone.slash",
          description: Text(
            "Create a one-time code from the child settings screen to pair their first device.")
        )
        .allowsHitTesting(false)
      }
    }
    .refreshable { await load() }
    .navigationTitle("\(child.value1.name)’s devices")
    .navigationBarTitleDisplayMode(.inline)
    .safeAreaInset(edge: .bottom) {
      Text("Re-pairing lets this device erase its Coop pairing and connect as a different child. It is locked by default.")
        .font(.footnote)
        .foregroundStyle(CoopTheme.foreground.opacity(0.68))
        .padding()
        .background(.ultraThinMaterial)
    }
    .task { await load() }
    .confirmationDialog(
      "Revoke \(revoking?.name ?? "this device")?",
      isPresented: revokeIsPresented,
      titleVisibility: .visible
    ) {
      Button("Revoke device", role: .destructive) { revoke() }
    } message: {
      Text("The child app will lose access immediately and must be paired again.")
    }
    .coopBackground()
  }

  private var revokeIsPresented: Binding<Bool> {
    Binding(
      get: { revoking != nil },
      set: { if !$0 { revoking = nil } }
    )
  }

  private func load() async {
    do {
      devices = try await model.childDevices(childID: child.value1.id)
    } catch {
      model.errorMessage = error.localizedDescription
    }
  }

  private func revoke() {
    guard let device = revoking else { return }
    Task {
      do {
        try await model.revokeChildDevice(id: device.id)
        devices.removeAll { $0.id == device.id }
      } catch {
        model.errorMessage = error.localizedDescription
      }
      revoking = nil
    }
  }

  private func update(_ device: Components.Schemas.Device, allowSelfUnpair: Bool) {
    guard let index = devices.firstIndex(where: { $0.id == device.id }) else { return }
    guard updatingDeviceIDs.insert(device.id).inserted else { return }
    devices[index].allowSelfUnpair = allowSelfUnpair
    Task {
      defer { updatingDeviceIDs.remove(device.id) }
      do {
        let updated = try await model.updateChildDevice(
          id: device.id,
          allowSelfUnpair: allowSelfUnpair
        )
        if let currentIndex = devices.firstIndex(where: { $0.id == device.id }) {
          devices[currentIndex] = updated
        }
      } catch {
        if let currentIndex = devices.firstIndex(where: { $0.id == device.id }) {
          devices[currentIndex].allowSelfUnpair = device.allowSelfUnpair
        }
        model.errorMessage = error.localizedDescription
      }
    }
  }

  private func lastSeen(_ device: Components.Schemas.Device) -> String {
    guard let lastSeenAt = device.lastSeenAt else { return "Never connected" }
    return "Last seen \(lastSeenAt.formatted(.relative(presentation: .named)))"
  }
}
