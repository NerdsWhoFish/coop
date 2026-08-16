import CoopKit
import SwiftUI

struct RequestQueueView: View {
  @Bindable var model: AppModel

  var body: some View {
    NavigationStack {
      Group {
        if model.requests.isEmpty {
          ContentUnavailableView(
            "All clear",
            systemImage: "checkmark.seal.fill",
            description: Text("New channel requests will show up here.")
          )
        } else {
          ScrollView {
            LazyVStack(spacing: 16) {
              ForEach(model.requests, id: \.id) { request in
                RequestCard(request: request, model: model)
              }
            }
            .padding()
          }
          .refreshable { await model.loadRequests() }
        }
      }
      .navigationTitle("Dispatch queue")
      .toolbar {
        ToolbarItem(placement: .topBarTrailing) {
          Text("\(model.requests.count) OPEN")
            .font(.caption2.weight(.black))
            .tracking(1.1)
            .foregroundStyle(model.requests.isEmpty ? CoopTheme.green : CoopTheme.orange)
        }
      }
      .coopBackground()
    }
  }
}

private struct RequestCard: View {
  enum Resolution {
    case approved
    case denied
  }

  let request: Components.Schemas.Request
  @Bindable var model: AppModel

  @Environment(\.accessibilityReduceMotion) private var reduceMotion
  @State private var isWorking = false
  @State private var resolution: Resolution?

  var body: some View {
    VStack(alignment: .leading, spacing: 18) {
      HStack(alignment: .firstTextBaseline) {
        Text((request.childName ?? "A child").uppercased())
          .font(.caption.weight(.black))
          .tracking(1.5)
          .foregroundStyle(CoopTheme.purple)
        Spacer()
        Text(request.createdAt, style: .relative)
          .font(.caption)
          .foregroundStyle(CoopTheme.foreground.opacity(0.55))
      }

      VStack(alignment: .leading, spacing: 5) {
        Text(request.channel.title)
          .font(.title3.bold())
        if let video = request.promptedByVideo {
          Text("Asked while looking for “\(video.title)”")
            .font(.subheadline)
            .foregroundStyle(CoopTheme.foreground.opacity(0.68))
        }
        if let note = request.note, !note.isEmpty {
          Text("“\(note)”")
            .font(.subheadline.italic())
            .foregroundStyle(CoopTheme.yellow)
            .padding(.top, 4)
        }
      }

      HStack(spacing: 10) {
        Menu {
          Button("Deny request") { decide(.denied, globally: false, blockChannel: false) }
          Button("Deny and block channel", role: .destructive) {
            decide(.denied, globally: false, blockChannel: true)
          }
        } label: {
          Label("Deny", systemImage: "xmark")
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.bordered)
        .tint(CoopTheme.red)

        Menu {
          Button("Clear for \(request.childName ?? "this child")") {
            decide(.approved, globally: false, blockChannel: false)
          }
          Button("Clear for every child") {
            decide(.approved, globally: true, blockChannel: false)
          }
        } label: {
          Label("Clear", systemImage: "checkmark")
            .fontWeight(.bold)
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .tint(CoopTheme.green)
        .foregroundStyle(CoopTheme.background)
      }
      .disabled(isWorking)
    }
    .padding(20)
    .background(CoopTheme.surface, in: .rect(cornerRadius: 20))
    .overlay(alignment: .center) {
      if let resolution {
        ResolutionStamp(resolution: resolution)
          .transition(.scale(scale: reduceMotion ? 1 : 1.7).combined(with: .opacity))
      }
    }
    .accessibilityElement(children: .contain)
  }

  private func decide(_ resolution: Resolution, globally: Bool, blockChannel: Bool) {
    isWorking = true
    Task {
      do {
        switch resolution {
        case .approved:
          try await model.approve(requestID: request.id, globally: globally)
        case .denied:
          try await model.deny(requestID: request.id, blockChannel: blockChannel)
        }
        withAnimation(reduceMotion ? nil : .spring(duration: 0.28, bounce: 0.35)) {
          self.resolution = resolution
        }
        if !reduceMotion {
          try await Task.sleep(for: .milliseconds(550))
        }
        model.dismiss(requestID: request.id)
      } catch {
        model.errorMessage = error.localizedDescription
        isWorking = false
      }
    }
  }
}

private struct ResolutionStamp: View {
  let resolution: RequestCard.Resolution

  var body: some View {
    Text(resolution == .approved ? "CLEARED" : "DENIED")
      .font(.title2.weight(.black))
      .tracking(3)
      .foregroundStyle(resolution == .approved ? CoopTheme.green : CoopTheme.red)
      .padding(.horizontal, 18)
      .padding(.vertical, 10)
      .background(CoopTheme.background.opacity(0.94), in: .rect(cornerRadius: 8))
      .overlay {
        RoundedRectangle(cornerRadius: 8)
          .stroke(lineWidth: 3)
          .foregroundStyle(resolution == .approved ? CoopTheme.green : CoopTheme.red)
      }
      .rotationEffect(.degrees(-7))
      .accessibilityLabel(resolution == .approved ? "Request approved" : "Request denied")
  }
}
