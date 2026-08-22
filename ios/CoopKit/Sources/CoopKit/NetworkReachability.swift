import Foundation
import Network
import Observation

/// Publishes when the device gets a usable network back, so a screen that
/// failed can reload itself instead of waiting for a child to find a button.
@MainActor
@Observable
public final class NetworkReachability {
  public static let shared = NetworkReachability()

  public private(set) var isOnline = true

  /// Bumped every time connectivity is restored. Screens holding a failure
  /// observe this and reload.
  public private(set) var reconnectCount = 0

  private var interfaces: Set<String> = []
  private let monitor = NWPathMonitor()

  private init() {
    monitor.pathUpdateHandler = { path in
      // Read the path here: NWPath must not cross into the actor hop.
      let online = path.status == .satisfied
      let names = Set(path.availableInterfaces.map(\.name))
      Task { @MainActor [weak self] in self?.apply(online: online, interfaces: names) }
    }
    monitor.start(queue: DispatchQueue(label: "fish.nerdswhofish.coop.reachability"))
  }

  /// A VPN tunnel dropping and re-establishing often leaves the path
  /// `satisfied` throughout, because the cellular interface never went away.
  /// The `utun` interface appearing again is the only signal that it is back.
  private func apply(online: Bool, interfaces names: Set<String>) {
    let cameBack = online && (!isOnline || (names != interfaces && !interfaces.isEmpty))
    isOnline = online
    interfaces = names
    if cameBack { reconnectCount += 1 }
  }
}
