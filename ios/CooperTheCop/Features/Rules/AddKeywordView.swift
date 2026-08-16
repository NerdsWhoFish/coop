import SwiftUI

struct AddKeywordView: View {
  @Bindable var model: AppModel
  let childID: String?
  let didSave: () async -> Void
  @Environment(\.dismiss) private var dismiss
  @State private var term = ""
  @State private var matchTitle = true
  @State private var matchTags = true
  @State private var matchDescription = false
  @State private var wholeWord = true
  @State private var isSaving = false

  var body: some View {
    NavigationStack {
      Form {
        Section("Hide videos containing") {
          TextField("Word or phrase", text: $term)
        }
        Section {
          Toggle("Titles", isOn: $matchTitle)
          Toggle("Tags", isOn: $matchTags)
          Toggle("Descriptions", isOn: $matchDescription)
        } header: {
          Text("Look in")
        } footer: {
          Text(
            "Descriptions often contain sponsor copy and boilerplate, so matching them can hide unrelated videos."
          )
        }
        Section {
          Toggle("Match whole words only", isOn: $wholeWord)
        } footer: {
          Text("Whole-word matching keeps “gun” from accidentally hiding “begun.”")
        }
      }
      .scrollContentBackground(.hidden)
      .background(CoopTheme.background)
      .navigationTitle(childID == nil ? "Family keyword" : "Child keyword")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar {
        ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
        ToolbarItem(placement: .confirmationAction) {
          Button("Add") { save() }
            .fontWeight(.bold)
            .disabled(
              term.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isSaving
                || !(matchTitle || matchTags || matchDescription))
        }
      }
      .coopBackground()
    }
  }

  private func save() {
    isSaving = true
    Task {
      do {
        try await model.createKeyword(
          term: term.trimmingCharacters(in: .whitespacesAndNewlines),
          childID: childID,
          matchTitle: matchTitle,
          matchTags: matchTags,
          matchDescription: matchDescription,
          wholeWord: wholeWord
        )
        await didSave()
        dismiss()
      } catch {
        model.errorMessage = error.localizedDescription
        isSaving = false
      }
    }
  }
}
