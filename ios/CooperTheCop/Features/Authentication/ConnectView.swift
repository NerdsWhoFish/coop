import SwiftUI

struct ConnectView: View {
  @Bindable var model: AppModel
  @FocusState private var addressFocused: Bool

  var body: some View {
    NavigationStack {
      GeometryReader { geometry in
        ScrollView {
          VStack(alignment: .leading, spacing: 28) {
            Spacer(minLength: 20)

            Image(systemName: "checkmark.shield.fill")
              .font(.system(size: 54, weight: .bold))
              .foregroundStyle(CoopTheme.cyan)
              .accessibilityHidden(true)

            VStack(alignment: .leading, spacing: 8) {
              Text("REPORT TO YOUR COOP")
                .font(.caption.weight(.black))
                .tracking(2.2)
                .foregroundStyle(CoopTheme.cyan)
              Text("The family dispatch desk.")
                .font(.largeTitle.bold())
              Text("Connect to the server your family controls. Your address stays on this device.")
                .font(.body)
                .foregroundStyle(CoopTheme.foreground.opacity(0.72))
            }

            VStack(alignment: .leading, spacing: 10) {
              Text("SERVER ADDRESS")
                .font(.caption2.weight(.bold))
                .tracking(1.4)
                .foregroundStyle(CoopTheme.muted)
              TextField("coop.example.com", text: $model.serverAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
                .submitLabel(.continue)
                .focused($addressFocused)
                .padding(16)
                .background(CoopTheme.surface, in: .rect(cornerRadius: 14))
                .onSubmit { Task { await model.connect() } }
            }

            Button {
              Task { await model.connect() }
            } label: {
              HStack {
                if model.isWorking {
                  ProgressView()
                }
                Text(model.isWorking ? "Checking in…" : "Connect securely")
                  .fontWeight(.bold)
                Spacer()
                Image(systemName: "arrow.right")
              }
              .padding(16)
              .foregroundStyle(CoopTheme.background)
              .background(CoopTheme.cyan, in: .rect(cornerRadius: 14))
            }
            .disabled(model.isWorking || model.serverAddress.isEmpty)
            .opacity(model.isWorking || model.serverAddress.isEmpty ? 0.55 : 1)

            Spacer(minLength: 20)
          }
          .padding(24)
          .frame(maxWidth: 720, minHeight: geometry.size.height)
          .frame(maxWidth: .infinity)
        }
        .scrollBounceBehavior(.basedOnSize)
      }
      .coopBackground()
    }
    .onAppear { addressFocused = model.serverAddress.isEmpty }
  }
}
