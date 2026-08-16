import CoopKit
import SwiftUI

struct SuppressionsView: View {
  let child: Components.Schemas.Child
  @Bindable var model: AppModel
  @State private var suppressions: [Components.Schemas.Suppression] = []
  @State private var resolving: Set<String> = []
  @State private var isLoading = false

  var body: some View {
    List(suppressions, id: \.id) { suppression in
      VStack(alignment: .leading, spacing: 12) {
        HStack(alignment: .top, spacing: 12) {
          AsyncImage(url: suppression.video.thumbnailUrl.flatMap(URL.init(string:))) { image in
            image.resizable().scaledToFill()
          } placeholder: {
            Rectangle().fill(CoopTheme.surface)
          }
          .frame(width: 112, height: 63)
          .clipShape(.rect(cornerRadius: 8))

          VStack(alignment: .leading, spacing: 4) {
            Text(suppression.video.title).font(.headline).lineLimit(3)
            Text(suppression.video.channelTitle ?? "Unknown channel")
              .font(.caption).foregroundStyle(CoopTheme.foreground.opacity(0.62))
          }
        }

        HStack {
          Label(
            "“\(suppression.term)” in \(suppression.matchedField?.rawValue ?? "metadata")",
            systemImage: "text.magnifyingglass"
          )
          .font(.caption.weight(.semibold))
          .foregroundStyle(CoopTheme.orange)
          Spacer()
          Menu("Allow") {
            Button("Allow for \(child.value1.name)") {
              resolve(suppression.id, familyWide: false)
            }
            Button("Allow for every child") { resolve(suppression.id, familyWide: true) }
          }
          .disabled(resolving.contains(suppression.id))
        }
      }
      .padding(.vertical, 6)
      .listRowBackground(CoopTheme.surface)
    }
    .scrollContentBackground(.hidden)
    .overlay {
      if suppressions.isEmpty {
        if isLoading {
          ProgressView()
        } else {
          ContentUnavailableView(
            "Nothing hidden",
            systemImage: "eye.slash",
            description: Text(
              "Videos hidden by keywords will appear here with the exact match that triggered the rule."
            )
          )
          .allowsHitTesting(false)
        }
      }
    }
    .refreshable { await load() }
    .navigationTitle("Hidden for \(child.value1.name)")
    .navigationBarTitleDisplayMode(.inline)
    .task { await load() }
    .coopBackground()
  }

  private func load() async {
    isLoading = true
    defer { isLoading = false }
    do {
      suppressions = try await model.suppressions(childID: child.value1.id)
    } catch {
      model.errorMessage = error.localizedDescription
    }
  }

  private func resolve(_ id: String, familyWide: Bool) {
    resolving.insert(id)
    Task {
      do {
        try await model.overrideSuppression(id: id, familyWide: familyWide)
        withAnimation { suppressions.removeAll { $0.id == id } }
      } catch {
        model.errorMessage = error.localizedDescription
      }
      resolving.remove(id)
    }
  }
}
