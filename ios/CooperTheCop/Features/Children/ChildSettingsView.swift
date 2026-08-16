import CoopKit
import SwiftUI

struct ChildSettingsView: View {
  let child: Components.Schemas.Child
  @Bindable var model: AppModel

  @State private var name: String
  @State private var shortsEnabled: Bool
  @State private var watchPageAutoplay: Bool
  @State private var videoSearchTiles: Bool
  @State private var dailySearchLimit: Int
  @State private var pairingCode: Components.Schemas.PairingCode?
  @State private var isWorking = false
  @State private var confirmingDeletion = false
  @Environment(\.dismiss) private var dismiss

  init(child: Components.Schemas.Child, model: AppModel) {
    self.child = child
    self.model = model
    _name = State(initialValue: child.value1.name)
    _shortsEnabled = State(initialValue: child.value2.shortsEnabled ?? false)
    _watchPageAutoplay = State(initialValue: child.value2.watchPageAutoplay ?? false)
    _videoSearchTiles = State(initialValue: child.value2.videoSearchTiles ?? false)
    _dailySearchLimit = State(initialValue: child.value2.dailySearchLimit ?? 0)
  }

  var body: some View {
    Form {
      Section("Profile") {
        TextField("Name", text: $name)
      }

      Section {
        Toggle("Show Shorts", isOn: $shortsEnabled)
        Toggle("Show videos in search", isOn: $videoSearchTiles)
        Stepper(value: $dailySearchLimit, in: 0...100) {
          LabeledContent("Daily searches", value: searchLimitLabel)
        }
      } header: {
        Text("Discovery")
      } footer: {
        Text("A zero search limit uses the family-wide budget without adding a child-specific cap.")
      }

      Section {
        Toggle("Autoplay watch pages", isOn: $watchPageAutoplay)
      } header: {
        Text("Playback")
      } footer: {
        Text(
          "When off, the child sees a local thumbnail until they tap play, so opening a video does not contact Google."
        )
      }

      Section("Device pairing") {
        Button("Create one-time code") { pair() }
          .disabled(isWorking)
        if let pairingCode {
          LabeledContent("Code") {
            Text(pairingCode.code)
              .font(.title3.monospaced().bold())
              .foregroundStyle(CoopTheme.yellow)
              .textSelection(.enabled)
          }
          Text("Expires \(pairingCode.expiresAt, style: .relative).")
            .font(.caption)
            .foregroundStyle(CoopTheme.foreground.opacity(0.65))
        }
        NavigationLink("Paired devices") {
          ChildDevicesView(child: child, model: model)
        }
      }

      Section("Audit") {
        NavigationLink("Hidden videos") {
          SuppressionsView(child: child, model: model)
        }
      }

      if model.parent?.role == .admin {
        Section {
          Button("Delete child profile", role: .destructive) { confirmingDeletion = true }
        }
      }
    }
    .scrollContentBackground(.hidden)
    .background(CoopTheme.background)
    .navigationTitle(child.value1.name)
    .navigationBarTitleDisplayMode(.inline)
    .toolbar {
      ToolbarItem(placement: .confirmationAction) {
        Button("Save") { save() }
          .fontWeight(.bold)
          .disabled(name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isWorking)
      }
    }
    .confirmationDialog(
      "Delete \(child.value1.name)?",
      isPresented: $confirmingDeletion,
      titleVisibility: .visible
    ) {
      Button("Delete child and all data", role: .destructive) { deleteChild() }
    } message: {
      Text("This permanently removes their devices, rules, requests, and activity.")
    }
    .coopBackground()
  }

  private var searchLimitLabel: String {
    dailySearchLimit == 0 ? "Family budget" : "\(dailySearchLimit)"
  }

  private func save() {
    isWorking = true
    let settings = Components.Schemas.ChildSettings(
      name: name.trimmingCharacters(in: .whitespacesAndNewlines),
      shortsEnabled: shortsEnabled,
      watchPageAutoplay: watchPageAutoplay,
      videoSearchTiles: videoSearchTiles,
      dailySearchLimit: dailySearchLimit
    )
    Task {
      do {
        try await model.updateChild(id: child.value1.id, settings: settings)
        isWorking = false
      } catch {
        model.errorMessage = error.localizedDescription
        isWorking = false
      }
    }
  }

  private func pair() {
    isWorking = true
    Task {
      do {
        pairingCode = try await model.createPairingCode(childID: child.value1.id)
        isWorking = false
      } catch {
        model.errorMessage = error.localizedDescription
        isWorking = false
      }
    }
  }

  private func deleteChild() {
    isWorking = true
    Task {
      do {
        try await model.deleteChild(id: child.value1.id)
        dismiss()
      } catch {
        model.errorMessage = error.localizedDescription
        isWorking = false
      }
    }
  }
}
