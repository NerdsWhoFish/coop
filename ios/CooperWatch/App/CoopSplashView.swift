import SwiftUI

struct CoopSplashView: View {
  let hasError: Bool
  let retry: () -> Void

  @State private var isBreathing = false

  var body: some View {
    VStack(spacing: 22) {
      ZStack {
        Circle()
          .fill(WatchTheme.purple.opacity(0.18))
          .frame(width: 150, height: 150)
          .blur(radius: 8)
          .scaleEffect(isBreathing ? 1.08 : 0.92)

        Image(systemName: "play.rectangle.on.rectangle.fill")
          .font(.system(size: 74, weight: .black))
          .foregroundStyle(WatchTheme.cyan, WatchTheme.purple)
          .shadow(color: WatchTheme.cyan.opacity(0.32), radius: 18)
      }
      .accessibilityHidden(true)

      VStack(spacing: 8) {
        Text("COOPER WATCH")
          .font(.largeTitle.weight(.black))
          .tracking(2)
        Text(hasError ? "Coop needs another try." : "Opening your Coop…")
          .font(.headline)
          .foregroundStyle(WatchTheme.foreground.opacity(0.72))
      }

      if hasError {
        Button("Try again", action: retry)
          .buttonStyle(.borderedProminent)
          .tint(WatchTheme.cyan)
          .foregroundStyle(WatchTheme.background)
      } else {
        ProgressView()
          .tint(WatchTheme.cyan)
          .controlSize(.large)
          .accessibilityLabel("Opening Cooper Watch")
      }
    }
    .padding(28)
    .watchBackground()
    .accessibilityIdentifier("coop-splash-screen")
    .onAppear { isBreathing = true }
    .animation(
      .easeInOut(duration: 1.4).repeatForever(autoreverses: true),
      value: isBreathing
    )
  }
}
