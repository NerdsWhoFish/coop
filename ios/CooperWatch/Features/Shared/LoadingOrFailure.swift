import CoopKit
import SwiftUI

/// What a child sees instead of a spinner that never stops.
struct LoadFailureView: View {
  let problem: CoopConnectionProblem
  let retry: () async -> Void

  @State private var isRetrying = false

  var body: some View {
    ContentUnavailableView {
      Label {
        Text("Something got tangled")
      } icon: {
        Image(systemName: problem.symbolName).foregroundStyle(WatchTheme.orange)
      }
    } description: {
      Text(problem.childMessage)
    } actions: {
      Button {
        isRetrying = true
        Task {
          await retry()
          isRetrying = false
        }
      } label: {
        if isRetrying {
          ProgressView().tint(WatchTheme.background)
        } else {
          Text("Try again").fontWeight(.bold)
        }
      }
      .buttonStyle(.borderedProminent)
      .tint(WatchTheme.cyan)
      .foregroundStyle(WatchTheme.background)
      .disabled(isRetrying)
    }
    .accessibilityIdentifier("load-failure-view")
  }
}

/// Every child screen shows this while it has nothing to display, so a failed
/// load offers a way out rather than becoming an endless spinner.
struct LoadingOrFailure: View {
  let failure: CoopConnectionProblem?
  let retry: () async -> Void

  var body: some View {
    Group {
      if let failure {
        LoadFailureView(problem: failure, retry: retry)
      } else {
        ProgressView()
      }
    }
    .reloadOnReconnect(if: failure != nil, retry)
  }
}

extension View {
  func parentBlockedPlaybackAlert(
    isPresented: Binding<Bool>,
    onDismiss: @escaping () -> Void = {}
  ) -> some View {
    alert("Your parents blocked this video", isPresented: isPresented) {
      Button("Watch something else", role: .cancel, action: onDismiss)
    } message: {
      Text("Choose another video to keep watching.")
    }
  }

  /// Reloads a failed screen once the network is back, so the tunnel dropping
  /// costs a child a moment rather than the video they picked.
  func reloadOnReconnect(
    if hasFailed: Bool,
    _ reload: @escaping () async -> Void
  ) -> some View {
    onChange(of: NetworkReachability.shared.reconnectCount) {
      guard hasFailed else { return }
      Task { await reload() }
    }
  }
}
