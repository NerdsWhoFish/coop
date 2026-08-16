import CoopKit
import SwiftUI

struct ChildRequestsView: View {
  @Bindable var model: ChildAppModel
  @State private var requests: [Components.Schemas.Request] = []

  var body: some View {
    List(requests, id: \.id) { request in
      HStack(spacing: 14) {
        Image(systemName: symbol(for: request.status))
          .font(.title2).foregroundStyle(color(for: request.status))
          .frame(width: 36)
        VStack(alignment: .leading, spacing: 4) {
          Text(request.channel.title).font(.headline)
          Text(label(for: request.status))
            .font(.caption.weight(.black)).tracking(1.1)
            .foregroundStyle(color(for: request.status))
        }
      }
      .padding(.vertical, 5)
      .listRowBackground(WatchTheme.surface)
    }
    .scrollContentBackground(.hidden)
    .overlay {
      if requests.isEmpty {
        ContentUnavailableView(
          "No asks yet",
          systemImage: "bell",
          description: Text("When you ask for a locked channel, its answer will show up here.")
        )
        .allowsHitTesting(false)
      }
    }
    .refreshable { await load() }
    .navigationTitle("My asks")
    .task { await load() }
    .watchBackground()
  }

  private func load() async {
    do { requests = try await model.requests() } catch {
      model.errorMessage = error.localizedDescription
    }
  }

  private func label(for status: Components.Schemas.RequestStatus) -> String {
    switch status {
    case .pending: "WAITING"
    case .approved: "APPROVED"
    case .denied: "NOT THIS TIME"
    }
  }

  private func symbol(for status: Components.Schemas.RequestStatus) -> String {
    switch status {
    case .pending: "clock.fill"
    case .approved: "checkmark.seal.fill"
    case .denied: "xmark.circle.fill"
    }
  }

  private func color(for status: Components.Schemas.RequestStatus) -> Color {
    switch status {
    case .pending: WatchTheme.orange
    case .approved: WatchTheme.green
    case .denied: WatchTheme.red
    }
  }
}
