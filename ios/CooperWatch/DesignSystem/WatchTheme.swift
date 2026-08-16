import SwiftUI

enum WatchTheme {
  static let background = Color(red: 40 / 255, green: 42 / 255, blue: 54 / 255)
  static let surface = Color(red: 68 / 255, green: 71 / 255, blue: 90 / 255)
  static let foreground = Color(red: 248 / 255, green: 248 / 255, blue: 242 / 255)
  static let muted = Color(red: 98 / 255, green: 114 / 255, blue: 164 / 255)
  static let cyan = Color(red: 139 / 255, green: 233 / 255, blue: 253 / 255)
  static let green = Color(red: 80 / 255, green: 250 / 255, blue: 123 / 255)
  static let orange = Color(red: 255 / 255, green: 184 / 255, blue: 108 / 255)
  static let pink = Color(red: 255 / 255, green: 121 / 255, blue: 198 / 255)
  static let purple = Color(red: 189 / 255, green: 147 / 255, blue: 249 / 255)
  static let red = Color(red: 255 / 255, green: 85 / 255, blue: 85 / 255)
  static let yellow = Color(red: 241 / 255, green: 250 / 255, blue: 140 / 255)
}

private struct WatchBackground: ViewModifier {
  func body(content: Content) -> some View {
    content
      .foregroundStyle(WatchTheme.foreground)
      .tint(WatchTheme.cyan)
      .frame(maxWidth: .infinity, maxHeight: .infinity)
      .background(
        ZStack {
          WatchTheme.background
          RadialGradient(
            colors: [WatchTheme.purple.opacity(0.16), .clear],
            center: .topTrailing,
            startRadius: 20,
            endRadius: 420
          )
        }.ignoresSafeArea()
      )
  }
}

extension View {
  func watchBackground() -> some View { modifier(WatchBackground()) }
}
