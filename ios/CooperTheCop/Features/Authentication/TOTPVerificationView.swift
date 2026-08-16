import CoreImage.CIFilterBuiltins
import SwiftUI
import UIKit

struct TOTPVerificationView: View {
  @Bindable var model: AppModel
  let challenge: AppModel.PendingAuthentication

  @State private var code = ""

  var body: some View {
    NavigationStack {
      Form {
        if let secret = challenge.secret, let provisioningURL = challenge.provisioningURL {
          Section {
            if let image = qrCode(for: provisioningURL) {
              HStack {
                Spacer()
                Image(uiImage: image)
                  .interpolation(.none)
                  .resizable()
                  .frame(width: 220, height: 220)
                  .accessibilityLabel("Authenticator setup QR code")
                Spacer()
              }
            }
            Text(secret)
              .font(.system(.body, design: .monospaced))
              .textSelection(.enabled)
          } header: {
            Text("Add Coop to your authenticator")
          } footer: {
            Text("Scan the code or enter the key manually. Coop will not show this secret again after verification.")
          }
        }

        Section("Six-digit code") {
          TextField("000000", text: $code)
            .keyboardType(.numberPad)
            .textContentType(.oneTimeCode)
        }

        Button("Verify and sign in") {
          Task { await model.verifyTOTP(code, challenge: challenge) }
        }
        .fontWeight(.bold)
        .disabled(model.isWorking || code.count != 6)
      }
      .scrollContentBackground(.hidden)
      .background(CoopTheme.background)
      .navigationTitle(challenge.secret == nil ? "Two-step verification" : "Secure your account")
      .toolbar {
        ToolbarItem(placement: .topBarLeading) {
          Button("Back") { model.destination = .authentication(needsSetup: false) }
        }
        if model.isWorking {
          ToolbarItem(placement: .topBarTrailing) { ProgressView() }
        }
      }
      .coopBackground()
    }
  }

  private func qrCode(for value: String) -> UIImage? {
    let filter = CIFilter.qrCodeGenerator()
    filter.message = Data(value.utf8)
    filter.correctionLevel = "M"
    guard let output = filter.outputImage else { return nil }
    let context = CIContext()
    guard let image = context.createCGImage(output, from: output.extent) else { return nil }
    return UIImage(cgImage: image)
  }
}
