import SwiftUI

public struct RequiredUpdateView: View {
  public enum Audience: Sendable {
    case parent
    case child
  }

  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  @Environment(\.openURL) private var openURL
  @State private var isBreathing = false

  private let release: AppRelease
  private let audience: Audience
  private let retry: () async -> Void

  public init(release: AppRelease, audience: Audience, retry: @escaping () async -> Void) {
    self.release = release
    self.audience = audience
    self.retry = retry
  }

  public var body: some View {
    ZStack {
      background.ignoresSafeArea()

      VStack(spacing: 26) {
        Spacer()
        updateMark

        VStack(spacing: 12) {
          Text(title)
            .font(.system(size: 36, weight: .black, design: .rounded))
            .multilineTextAlignment(.center)
            .foregroundStyle(.white)
          Text(message)
            .font(.system(.title3, design: .rounded, weight: .medium))
            .multilineTextAlignment(.center)
            .foregroundStyle(.white.opacity(0.78))
            .fixedSize(horizontal: false, vertical: true)
        }

        VStack(spacing: 14) {
          Button(action: install) {
            Label(buttonTitle, systemImage: "arrow.down.app.fill")
              .font(.system(.headline, design: .rounded, weight: .bold))
              .frame(maxWidth: .infinity)
              .padding(.vertical, 7)
          }
          .buttonStyle(.borderedProminent)
          .buttonBorderShape(.roundedRectangle(radius: 16))
          .controlSize(.large)
          .tint(accent)
          .foregroundStyle(Color(red: 0.07, green: 0.08, blue: 0.11))
          .accessibilityIdentifier("required-update-install")
          .accessibilityLabel(buttonTitle)

          Button("I installed it — check again") {
            Task { await retry() }
          }
          .font(.subheadline.weight(.semibold))
          .foregroundStyle(.white.opacity(0.75))
        }
        .frame(maxWidth: 440)

        Text("Build \(currentBuild)  •  Update \(release.build)")
          .font(.caption.monospacedDigit().weight(.semibold))
          .foregroundStyle(.white.opacity(0.48))
        Spacer()
      }
      .padding(.horizontal, 28)
      .padding(.vertical, 24)
    }
    .onAppear {
      guard !reduceMotion else { return }
      withAnimation(.easeInOut(duration: 1.6).repeatForever(autoreverses: true)) {
        isBreathing = true
      }
    }
    .accessibilityIdentifier("required-update-screen")
  }

  private var updateMark: some View {
    ZStack {
      Circle()
        .stroke(accent.opacity(0.18), lineWidth: 18)
        .frame(width: 154, height: 154)
        .scaleEffect(isBreathing ? 1.08 : 0.94)
      Circle()
        .fill(accent.opacity(0.16))
        .frame(width: 120, height: 120)
      Image(systemName: audience == .child ? "sparkles" : "arrow.triangle.2.circlepath")
        .font(.system(size: 52, weight: .black))
        .foregroundStyle(accent)
        .rotationEffect(.degrees(isBreathing && audience == .parent ? 10 : 0))
    }
    .accessibilityHidden(true)
  }

  private var background: some View {
    LinearGradient(
      colors: [
        Color(red: 0.08, green: 0.09, blue: 0.13),
        audience == .child
          ? Color(red: 0.16, green: 0.12, blue: 0.24)
          : Color(red: 0.08, green: 0.15, blue: 0.18),
      ],
      startPoint: .topLeading,
      endPoint: .bottomTrailing
    )
  }

  private var accent: Color {
    audience == .child
      ? Color(red: 0.55, green: 0.93, blue: 0.96)
      : Color(red: 0.31, green: 0.90, blue: 0.64)
  }

  private var title: String {
    audience == .child ? "Fresh Coop gear!" : "A Coop update is ready"
  }

  private var message: String {
    audience == .child
      ? "Ask a grown-up to tap the big button. Your shows, likes, and pairing stay right where they are."
      : "Update to keep approvals, playback controls, and family settings working together. Your setup stays exactly where it is."
  }

  private var buttonTitle: String {
    audience == .child ? "Get the update" : "Update Coop"
  }

  private var currentBuild: String {
    Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "unknown"
  }

  private func install() {
    guard let installURL = release.installURL else {
      if let installerURL = release.installerURL { openURL(installerURL) }
      return
    }
    openURL(installURL) { accepted in
      guard !accepted, let installerURL = release.installerURL else { return }
      openURL(installerURL)
    }
  }
}
