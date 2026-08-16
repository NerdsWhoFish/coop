import CoopKit
import SwiftUI

struct ParentScopeView: View {
  let parent: Components.Schemas.Parent
  @Bindable var model: AppModel
  let didChange: () async -> Void
  @Environment(\.dismiss) private var dismiss
  @State private var childIDs: Set<String>
  @State private var confirmingRemoval = false

  init(parent: Components.Schemas.Parent, model: AppModel, didChange: @escaping () async -> Void) {
    self.parent = parent
    self.model = model
    self.didChange = didChange
    _childIDs = State(initialValue: Set(parent.scopedChildIds ?? []))
  }

  var body: some View {
    Form {
      Section("Account") {
        LabeledContent("Email", value: parent.email)
        LabeledContent("Role", value: parent.role.rawValue.capitalized)
      }
      if parent.role == .parent {
        Section("Can manage") {
          ForEach(model.children, id: \.value1.id) { child in
            Toggle(child.value1.name, isOn: selection(for: child.value1.id))
          }
        }
      }
      if parent.id != model.parent?.id {
        Section {
          Button("Remove parent", role: .destructive) { confirmingRemoval = true }
        }
      }
    }
    .scrollContentBackground(.hidden)
    .background(CoopTheme.background)
    .navigationTitle("Parent access")
    .toolbar {
      if parent.role == .parent {
        Button("Save") { save() }.fontWeight(.bold).disabled(childIDs.isEmpty)
      }
    }
    .confirmationDialog(
      "Remove \(parent.email)?", isPresented: $confirmingRemoval, titleVisibility: .visible
    ) {
      Button("Remove parent", role: .destructive) { remove() }
    } message: {
      Text("Their active sessions will stop working immediately.")
    }
    .coopBackground()
  }

  private func selection(for id: String) -> Binding<Bool> {
    Binding(
      get: { childIDs.contains(id) },
      set: { selected in
        if selected {
          childIDs.insert(id)
        } else {
          childIDs.remove(id)
        }
      }
    )
  }

  private func save() {
    Task {
      do {
        try await model.setParentScope(parentID: parent.id, childIDs: Array(childIDs))
        await didChange()
        dismiss()
      } catch { model.errorMessage = error.localizedDescription }
    }
  }

  private func remove() {
    Task {
      do {
        try await model.deleteParent(id: parent.id)
        await didChange()
        dismiss()
      } catch { model.errorMessage = error.localizedDescription }
    }
  }
}
