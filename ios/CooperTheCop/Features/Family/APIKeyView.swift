import SwiftUI

struct APIKeyView: View {
  @Bindable var model: AppModel
  let didSave: () async -> Void
  @Environment(\.dismiss) private var dismiss
  @State private var apiKey = ""
  @State private var isWorking = false

  var body: some View {
    NavigationStack {
      Form {
        Section {
          SecureField("YouTube Data API key", text: $apiKey)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        } footer: {
          Text(
            "The server validates this key with YouTube, encrypts it at rest, and never returns it to the app."
          )
        }
      }
      .scrollContentBackground(.hidden)
      .background(CoopTheme.background)
      .navigationTitle("YouTube API")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar {
        ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
        ToolbarItem(placement: .confirmationAction) {
          Button("Save") { save() }.fontWeight(.bold).disabled(apiKey.isEmpty || isWorking)
        }
      }
      .coopBackground()
    }
  }

  private func save() {
    isWorking = true
    Task {
      do {
        try await model.setFamilyAPIKey(apiKey.trimmingCharacters(in: .whitespacesAndNewlines))
        await didSave()
        dismiss()
      } catch {
        model.errorMessage = error.localizedDescription
        isWorking = false
      }
    }
  }
}
