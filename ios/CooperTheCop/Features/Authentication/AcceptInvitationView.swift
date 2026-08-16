import SwiftUI

struct AcceptInvitationView: View {
  @Bindable var model: AppModel
  @Environment(\.dismiss) private var dismiss
  @State private var code = ""
  @State private var password = ""
  @State private var isWorking = false

  var body: some View {
    NavigationStack {
      Form {
        Section {
          TextField("Invitation code", text: $code)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
          SecureField("Choose a password", text: $password)
            .textContentType(.newPassword)
        } footer: {
          Text("The code works once and expires after seven days.")
        }
      }
      .scrollContentBackground(.hidden)
      .background(CoopTheme.background)
      .navigationTitle("Join this Coop")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar {
        ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
        ToolbarItem(placement: .confirmationAction) {
          Button("Join") { accept() }
            .fontWeight(.bold)
            .disabled(code.isEmpty || password.isEmpty || isWorking)
        }
      }
      .coopBackground()
    }
  }

  private func accept() {
    isWorking = true
    Task {
      do {
        try await model.acceptInvitation(
          code: code.trimmingCharacters(in: .whitespacesAndNewlines),
          password: password
        )
        dismiss()
      } catch {
        model.errorMessage = error.localizedDescription
        isWorking = false
      }
    }
  }
}
