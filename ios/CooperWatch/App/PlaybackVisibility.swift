import SwiftUI

private struct PlaybackSurfaceActiveKey: EnvironmentKey {
  static let defaultValue = true
}

extension EnvironmentValues {
  var playbackSurfaceActive: Bool {
    get { self[PlaybackSurfaceActiveKey.self] }
    set { self[PlaybackSurfaceActiveKey.self] = newValue }
  }
}
