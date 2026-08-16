import SwiftUI

struct PairingView: View {
  @Bindable var model: ChildAppModel
  @State private var code = ""

  var body: some View {
    ScrollView {
      VStack(spacing: 24) {
        Image(systemName: "play.rectangle.on.rectangle.fill")
          .font(.system(size: 66, weight: .black))
          .foregroundStyle(WatchTheme.cyan, WatchTheme.purple)
          .accessibilityHidden(true)

        VStack(spacing: 8) {
          Text("COOPER WATCH")
            .font(.largeTitle.weight(.black))
            .tracking(2)
          Text("Your videos. Your channels. Grown-up approved.")
            .font(.title3.weight(.semibold))
            .multilineTextAlignment(.center)
            .foregroundStyle(WatchTheme.foreground.opacity(0.72))
        }

        VStack(spacing: 14) {
          TextField("Coop server", text: $model.serverAddress)
            .textContentType(.URL)
            .textInputAutocapitalization(.never)
            .keyboardType(.URL)
            .textFieldStyle(.roundedBorder)
          TextField("Pairing code", text: $code)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .font(.title3.monospaced().bold())
            .textFieldStyle(.roundedBorder)
          Button {
            Task { await model.pair(code: code.trimmingCharacters(in: .whitespacesAndNewlines)) }
          } label: {
            Label("Open my Coop", systemImage: "sparkles")
              .font(.headline).frame(maxWidth: .infinity).padding(.vertical, 7)
          }
          .buttonStyle(.borderedProminent)
          .tint(WatchTheme.green)
          .foregroundStyle(WatchTheme.background)
          .disabled(model.serverAddress.isEmpty || code.isEmpty || model.isWorking)
        }
        .padding(20)
        .background(WatchTheme.surface, in: .rect(cornerRadius: 22))

        Text("Ask a grown-up to make a one-time code in Cooper The Cop.")
          .font(.footnote)
          .multilineTextAlignment(.center)
          .foregroundStyle(WatchTheme.foreground.opacity(0.58))
      }
      .frame(maxWidth: 560)
      .padding(28)
    }
    .watchBackground()
  }
}
