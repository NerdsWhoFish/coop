import SwiftUI

struct AuthenticationView: View {
  @Bindable var model: AppModel
  let needsSetup: Bool

  @State private var familyName = ""
  @State private var email = ""
  @State private var password = ""
  @State private var showingInvitation = false

  var body: some View {
    NavigationStack {
      Form {
        Section {
          if needsSetup {
            TextField("Family name", text: $familyName)
              .textContentType(.organizationName)
          }
          TextField("Email", text: $email)
            .textContentType(.emailAddress)
            .textInputAutocapitalization(.never)
            .keyboardType(.emailAddress)
          SecureField("Password", text: $password)
            .textContentType(needsSetup ? .newPassword : .password)
        } header: {
          Text(needsSetup ? "Create the first admin" : "Parent credentials")
        } footer: {
          if needsSetup {
            Text("This account controls the family API key and can invite other adults.")
          }
        }

        Section {
          Button(needsSetup ? "Open the dispatch desk" : "Sign in") {
            Task {
              if needsSetup {
                await model.setUp(
                  familyName: familyName,
                  email: email,
                  password: password
                )
              } else {
                await model.logIn(email: email, password: password)
              }
            }
          }
          .fontWeight(.bold)
          .disabled(
            model.isWorking || email.isEmpty || password.isEmpty
              || (needsSetup && familyName.isEmpty))
          if !needsSetup {
            Button("Use an invitation") { showingInvitation = true }
          }
        }
      }
      .scrollContentBackground(.hidden)
      .background(CoopTheme.background)
      .navigationTitle(needsSetup ? "Set up your Coop" : "Welcome back")
      .toolbar {
        ToolbarItem(placement: .topBarLeading) {
          Button("Server") { model.destination = .connecting }
        }
        if model.isWorking {
          ToolbarItem(placement: .topBarTrailing) { ProgressView() }
        }
      }
      .coopBackground()
      .sheet(isPresented: $showingInvitation) {
        AcceptInvitationView(model: model)
      }
    }
  }
}
