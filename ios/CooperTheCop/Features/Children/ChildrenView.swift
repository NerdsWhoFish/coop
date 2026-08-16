import CoopKit
import SwiftUI

struct ChildrenView: View {
  @Bindable var model: AppModel
  @State private var isAddingChild = false

  var body: some View {
    NavigationStack {
      List(model.children, id: \.value1.id) { child in
        NavigationLink {
          ChildSettingsView(child: child, model: model)
        } label: {
          ChildRow(child: child)
        }
        .listRowBackground(CoopTheme.surface)
      }
      .scrollContentBackground(.hidden)
      .overlay {
        if model.children.isEmpty {
          ContentUnavailableView(
            "No child profiles",
            systemImage: "figure.2.and.child.holdinghands",
            description: Text("Add a child to configure what they can find and watch.")
          )
          .allowsHitTesting(false)
        }
      }
      .refreshable { await model.loadChildren() }
      .navigationTitle("Children")
      .toolbar {
        if model.parent?.role == .admin {
          ToolbarItem(placement: .topBarTrailing) {
            Button("Add child", systemImage: "plus") { isAddingChild = true }
          }
        }
      }
      .sheet(isPresented: $isAddingChild) {
        AddChildView(model: model)
      }
      .coopBackground()
    }
  }
}

private struct ChildRow: View {
  let child: Components.Schemas.Child

  var body: some View {
    HStack(spacing: 14) {
      Image(systemName: "person.crop.circle.fill")
        .font(.largeTitle)
        .foregroundStyle(CoopTheme.purple)
        .accessibilityHidden(true)
      VStack(alignment: .leading, spacing: 3) {
        Text(child.value1.name)
          .font(.headline)
        Text(status)
          .font(.caption)
          .foregroundStyle(CoopTheme.foreground.opacity(0.65))
      }
    }
    .padding(.vertical, 6)
  }

  private var status: String {
    let devices = child.value1.deviceCount ?? 0
    let requests = child.value1.pendingRequestCount ?? 0
    return
      "\(devices) device\(devices == 1 ? "" : "s") · \(requests) open request\(requests == 1 ? "" : "s")"
  }
}

private struct AddChildView: View {
  @Bindable var model: AppModel
  @Environment(\.dismiss) private var dismiss
  @State private var name = ""
  @State private var isSaving = false

  var body: some View {
    NavigationStack {
      Form {
        TextField("Name", text: $name)
          .textContentType(.name)
      }
      .scrollContentBackground(.hidden)
      .background(CoopTheme.background)
      .navigationTitle("Add child")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar {
        ToolbarItem(placement: .cancellationAction) {
          Button("Cancel") { dismiss() }
        }
        ToolbarItem(placement: .confirmationAction) {
          Button("Add") { save() }
            .fontWeight(.bold)
            .disabled(name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isSaving)
        }
      }
      .coopBackground()
    }
  }

  private func save() {
    isSaving = true
    Task {
      do {
        try await model.createChild(name: name.trimmingCharacters(in: .whitespacesAndNewlines))
        dismiss()
      } catch {
        model.errorMessage = error.localizedDescription
        isSaving = false
      }
    }
  }
}
