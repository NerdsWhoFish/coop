import CoopKit
import SwiftUI

struct FamilyView: View {
  @Bindable var model: AppModel
  @State private var family: Components.Schemas.Family?
  @State private var quota: [Components.Schemas.QuotaStatus] = []
  @State private var parents: [Components.Schemas.Parent] = []
  @State private var invitation: Components.Schemas.Invitation?
  @State private var showingInvite = false
  @State private var showingAPIKey = false
  @State private var confirmingDeletion = false

  var body: some View {
    NavigationStack {
      List {
        if let family {
          Section("Instance") {
            LabeledContent("Family", value: family.name)
            LabeledContent("Timezone", value: family.timezone)
            LabeledContent("YouTube API") {
              Label(
                family.apiKeyConfigured ? "Connected" : "Needs a key",
                systemImage: family.apiKeyConfigured
                  ? "checkmark.circle.fill" : "exclamationmark.triangle.fill"
              )
              .foregroundStyle(family.apiKeyConfigured ? CoopTheme.green : CoopTheme.orange)
            }
            if model.parent?.role == .admin {
              Button(family.apiKeyConfigured ? "Replace API key" : "Set API key") {
                showingAPIKey = true
              }
            }
          }
        }

        if !quota.isEmpty {
          Section("Today’s YouTube budget") {
            ForEach(quota, id: \.purpose) { item in
              VStack(alignment: .leading, spacing: 7) {
                LabeledContent(
                  item.purpose.rawValue.capitalized, value: "\(item.used) / \(item.budget)")
                ProgressView(value: Double(item.used), total: Double(max(item.budget, 1)))
                  .tint(item.used >= item.budget ? CoopTheme.red : CoopTheme.cyan)
              }
            }
          }
        }

        Section("Parents") {
          if model.parent?.role == .admin {
            ForEach(parents, id: \.id) { parent in
              NavigationLink {
                ParentScopeView(parent: parent, model: model) { await load() }
              } label: {
                VStack(alignment: .leading, spacing: 3) {
                  Text(parent.email)
                  Text(parent.role.rawValue.uppercased())
                    .font(.caption2.weight(.black)).tracking(1.1)
                    .foregroundStyle(parent.role == .admin ? CoopTheme.purple : CoopTheme.cyan)
                }
              }
            }
            Button("Invite another parent", systemImage: "person.badge.plus") {
              showingInvite = true
            }
          } else if let parent = model.parent {
            LabeledContent(parent.email, value: "Parent")
          }
        }

        Section("History") {
          NavigationLink("Policy and security activity", destination: AuditView(model: model))
        }

        Section {
          Button("Sign out", role: .destructive) { model.logOut() }
          if model.parent?.role == .admin {
            Button("Delete family and all data", role: .destructive) {
              confirmingDeletion = true
            }
          }
        }
      }
      .scrollContentBackground(.hidden)
      .background(CoopTheme.background)
      .navigationTitle("Family desk")
      .refreshable { await load() }
      .task { await load() }
      .sheet(isPresented: $showingInvite) {
        InviteParentView(model: model) { created in
          invitation = created
          await load()
        }
      }
      .sheet(isPresented: invitationIsPresented) {
        if let invitation { InvitationResultView(invitation: invitation) }
      }
      .sheet(isPresented: $showingAPIKey) {
        APIKeyView(model: model) { await load() }
      }
      .alert("Delete this family?", isPresented: $confirmingDeletion) {
        Button("Delete permanently", role: .destructive) {
          Task {
            do {
              try await model.deleteFamily()
            } catch {
              model.errorMessage = error.localizedDescription
            }
          }
        }
        Button("Cancel", role: .cancel) {}
      } message: {
        Text(
          "This permanently deletes every parent account, child profile, rule, activity record, and audit event. This cannot be undone."
        )
      }
      .coopBackground()
    }
  }

  private func load() async {
    do {
      async let familyLoad = model.family()
      async let quotaLoad = model.familyQuota()
      if model.parent?.role == .admin { parents = try await model.parents() }
      (family, quota) = try await (familyLoad, quotaLoad)
    } catch {
      model.errorMessage = error.localizedDescription
    }
  }

  private var invitationIsPresented: Binding<Bool> {
    Binding(
      get: { invitation != nil },
      set: { if !$0 { invitation = nil } }
    )
  }
}

private struct AuditView: View {
  let model: AppModel
  @State private var events: [Components.Schemas.AuditEvent] = []

  var body: some View {
    List(events, id: \.id) { event in
      VStack(alignment: .leading, spacing: 5) {
        Text(title(for: event.action))
          .font(.headline)
        Text("\(event.targetType) · \(event.targetId)")
          .font(.caption.monospaced())
          .foregroundStyle(.secondary)
        Text(event.createdAt.formatted(date: .abbreviated, time: .shortened))
          .font(.caption)
          .foregroundStyle(.secondary)
      }
      .padding(.vertical, 3)
    }
    .navigationTitle("Activity history")
    .overlay {
      if events.isEmpty {
        ContentUnavailableView("No changes yet", systemImage: "clock.arrow.circlepath")
          .allowsHitTesting(false)
      }
    }
    .refreshable { await load() }
    .task { await load() }
  }

  private func load() async {
    do {
      events = try await model.auditEvents()
    } catch {
      model.errorMessage = error.localizedDescription
    }
  }

  private func title(for action: String) -> String {
    action.replacingOccurrences(of: ".", with: " ").replacingOccurrences(of: "_", with: " ")
      .capitalized
  }
}
