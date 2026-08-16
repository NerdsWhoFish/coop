import CoopKit
import SwiftUI

struct InviteParentView: View {
  @Bindable var model: AppModel
  let didCreate: (Components.Schemas.Invitation) async -> Void
  @Environment(\.dismiss) private var dismiss
  @State private var email = ""
  @State private var isAdmin = false
  @State private var childIDs: Set<String> = []
  @State private var isWorking = false

  var body: some View {
    NavigationStack {
      Form {
        Section("Account") {
          TextField("Email", text: $email)
            .textContentType(.emailAddress)
            .textInputAutocapitalization(.never)
            .keyboardType(.emailAddress)
          Toggle("Family administrator", isOn: $isAdmin)
        }
        if !isAdmin {
          Section("Can manage") {
            ForEach(model.children, id: \.value1.id) { child in
              Toggle(child.value1.name, isOn: selection(for: child.value1.id))
            }
          }
        }
      }
      .scrollContentBackground(.hidden)
      .background(CoopTheme.background)
      .navigationTitle("Invite parent")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar {
        ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
        ToolbarItem(placement: .confirmationAction) {
          Button("Create invite") { create() }
            .fontWeight(.bold)
            .disabled(email.isEmpty || isWorking || (!isAdmin && childIDs.isEmpty))
        }
      }
      .coopBackground()
    }
  }

  private func selection(for id: String) -> Binding<Bool> {
    Binding(
      get: { childIDs.contains(id) },
      set: { selected in
        if selected { childIDs.insert(id) } else { childIDs.remove(id) }
      }
    )
  }

  private func create() {
    isWorking = true
    Task {
      do {
        if let invitation = try await model.inviteParent(
          email: email.trimmingCharacters(in: .whitespacesAndNewlines),
          admin: isAdmin,
          childIDs: Array(childIDs)
        ) {
          dismiss()
          await didCreate(invitation)
        }
      } catch {
        model.errorMessage = error.localizedDescription
        isWorking = false
      }
    }
  }
}

struct InvitationResultView: View {
  let invitation: Components.Schemas.Invitation
  @Environment(\.dismiss) private var dismiss

  var body: some View {
    NavigationStack {
      VStack(spacing: 22) {
        Image(systemName: "person.crop.circle.badge.checkmark")
          .font(.system(size: 58)).foregroundStyle(CoopTheme.green)
        Text("Invite ready").font(.largeTitle.bold())
        Text(invitation.email).foregroundStyle(CoopTheme.foreground.opacity(0.7))
        Text(invitation.code)
          .font(.title3.monospaced().bold())
          .textSelection(.enabled)
          .padding().background(CoopTheme.surface, in: .rect(cornerRadius: 12))
        Text(
          "This secret works once and expires \(invitation.expiresAt, style: .relative). Send it privately."
        )
        .multilineTextAlignment(.center)
        ShareLink(item: invitation.code) {
          Label("Share invitation code", systemImage: "square.and.arrow.up")
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
      }
      .padding(28)
      .navigationTitle("Parent invitation")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar { Button("Done") { dismiss() } }
      .coopBackground()
    }
  }
}
