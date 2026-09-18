//go:build integration

package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/policy"
	"github.com/nerdswhofish/coop/internal/youtube"
)

func TestDeleteParentPreservesPolicyAndHistory(t *testing.T) {
	for _, self := range []bool{false, true} {
		name := "another parent"
		if self {
			name = "self"
		}
		t.Run(name, func(t *testing.T) {
			db := migratedDB(t)
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Second)
			accounts := NewAccounts(db, fixedClock(now))
			familyID := newFamily(t, db)
			admin, err := accounts.CreateParent(ctx, familyID, uuid.NewString()+"@example.com", "hash", domain.RoleAdmin, nil)
			if err != nil {
				t.Fatal(err)
			}
			child, err := accounts.CreateChild(ctx, familyID, "Child", "", admin.ID)
			if err != nil {
				t.Fatal(err)
			}
			parent, err := accounts.CreateParent(ctx, familyID, uuid.NewString()+"@example.com", "hash", domain.RoleAdmin, []uuid.UUID{child.ID})
			if err != nil {
				t.Fatal(err)
			}
			actorID := admin.ID
			if self {
				actorID = parent.ID
			}
			channelID := uuid.NewString()
			catalog := NewCatalog(db, fixedClock(now))
			if err := catalog.UpsertChannels(ctx, []youtube.Channel{{ID: channelID}}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Delete(&Channel{}, "id = ?", channelID) })
			allowedID, blockedID, overrideID := uuid.NewString(), uuid.NewString(), uuid.NewString()
			if err := catalog.UpsertVideos(ctx, []youtube.Video{
				{ID: allowedID, ChannelID: channelID, Embeddable: true},
				{ID: blockedID, ChannelID: channelID, Embeddable: true},
				{ID: overrideID, ChannelID: channelID, Embeddable: true},
			}); err != nil {
				t.Fatal(err)
			}
			rules := NewRules(db, fixedClock(now))
			if err := rules.AllowGlobally(ctx, familyID, channelID, parent.ID); err != nil {
				t.Fatal(err)
			}
			if err := rules.AllowForChild(ctx, child.ID, channelID, parent.ID); err != nil {
				t.Fatal(err)
			}
			if err := rules.BlockVideoForChild(ctx, child.ID, blockedID, parent.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := rules.CreateKeyword(ctx, Keyword{FamilyID: familyID, Term: "scary", MatchTitle: true}, parent.ID); err != nil {
				t.Fatal(err)
			}
			if err := rules.CreateOverride(ctx, VideoOverride{FamilyID: familyID, ChildID: &child.ID, VideoID: overrideID}, parent.ID); err != nil {
				t.Fatal(err)
			}
			request := Request{ChildID: child.ID, ChannelID: channelID, Status: domain.RequestApproved,
				DecidedBy: &parent.ID, DecidedAt: &now, DecisionNote: "Approved before removing account"}
			if err := db.Create(&request).Error; err != nil {
				t.Fatal(err)
			}
			token := uuid.NewString()
			if _, err := accounts.CreateSession(ctx, parent.ID, token, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if err := accounts.CreateAuthChallenge(ctx, ParentAuthChallenge{ParentID: parent.ID,
				TokenHash: uuid.NewString(), Purpose: AuthPurposeLogin, ExpiresAt: now.Add(time.Minute)}); err != nil {
				t.Fatal(err)
			}
			if err := accounts.SaveParentPushToken(ctx, familyID, parent.ID, uuid.NewString()); err != nil {
				t.Fatal(err)
			}
			invitation, err := accounts.CreateParentInvitation(ctx, ParentInvitation{FamilyID: familyID,
				Email: uuid.NewString() + "@example.com", Role: domain.RoleParent, TokenHash: uuid.NewString(),
				CreatedBy: parent.ID, ExpiresAt: now.Add(time.Hour)}, []uuid.UUID{child.ID})
			if err != nil {
				t.Fatal(err)
			}
			before, err := rules.Evaluator(ctx, familyID, child.ID)
			if err != nil {
				t.Fatal(err)
			}
			var auditBefore int64
			if err := db.Model(&AuditEvent{}).Where("actor_parent_id = ?", parent.ID).Count(&auditBefore).Error; err != nil {
				t.Fatal(err)
			}
			if err := accounts.DeleteParent(ctx, familyID, parent.ID, actorID); err != nil {
				t.Fatalf("DeleteParent() = %v", err)
			}
			if err := db.MigrateDown(); err != nil {
				t.Fatalf("rollback with deleted actors: %v", err)
			}
			if err := db.Migrate(); err != nil {
				t.Fatalf("reapply with deleted actors: %v", err)
			}
			after, err := rules.Evaluator(ctx, familyID, child.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, check := range []struct {
				video   policy.Video
				verdict policy.Verdict
			}{
				{policy.Video{ID: allowedID, ChannelID: channelID, Embeddable: true}, policy.VerdictServe},
				{policy.Video{ID: blockedID, ChannelID: channelID, Embeddable: true}, policy.VerdictVideoBlocked},
				{policy.Video{ID: overrideID, ChannelID: channelID, Title: "scary", Embeddable: true}, policy.VerdictServe},
			} {
				if got := before.Video(check.video).Verdict; got != check.verdict {
					t.Fatalf("fixture verdict = %s, want %s", got, check.verdict)
				}
				if got, want := after.Video(check.video), before.Video(check.video); !reflect.DeepEqual(got, want) {
					t.Errorf("policy changed after deleting parent: got %+v, want %+v", got, want)
				}
			}
			for _, check := range []struct {
				model any
				where string
				id    uuid.UUID
			}{
				{&AllowGlobal{}, "family_id = ? AND approved_by IS NULL", familyID},
				{&AllowChild{}, "child_id = ? AND approved_by IS NULL", child.ID},
				{&VideoBlock{}, "child_id = ? AND created_by IS NULL", child.ID},
				{&VideoOverride{}, "family_id = ? AND created_by IS NULL", familyID},
			} {
				var count int64
				if err := db.Model(check.model).Where(check.where, check.id).Count(&count).Error; err != nil || count != 1 {
					t.Errorf("retained %T count = %d, error = %v", check.model, count, err)
				}
			}
			var retained Request
			if err := db.First(&retained, "id = ?", request.ID).Error; err != nil {
				t.Fatal(err)
			}
			if retained.DecidedBy != nil || retained.Status != request.Status || retained.DecisionNote != request.DecisionNote ||
				retained.DecidedAt == nil || !retained.DecidedAt.Equal(now) {
				t.Errorf("request history changed: %+v", retained)
			}
			for _, model := range []any{&ParentSession{}, &ParentScope{}, &ParentAuthChallenge{}, &PushToken{}} {
				var count int64
				if err := db.Model(model).Where("parent_id = ?", parent.ID).Count(&count).Error; err != nil || count != 0 {
					t.Errorf("remaining %T count = %d, error = %v", model, count, err)
				}
			}
			var invitations int64
			if err := db.Model(&ParentInvitation{}).Where("created_by = ?", parent.ID).Count(&invitations).Error; err != nil || invitations != 0 {
				t.Errorf("remaining invitations = %d, error = %v", invitations, err)
			}
			if err := db.Model(&ParentInvitationScope{}).Where("invitation_id = ?", invitation.ID).Count(&invitations).Error; err != nil || invitations != 0 {
				t.Errorf("remaining invitation scopes = %d, error = %v", invitations, err)
			}
			if _, _, err := accounts.SessionByToken(ctx, token); !errors.Is(err, ErrNotFound) {
				t.Errorf("deleted parent's session = %v, want not found", err)
			}
			if _, err := accounts.Parent(ctx, parent.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("deleted parent = %v, want not found", err)
			}
			var auditAfter int64
			if err := db.Model(&AuditEvent{}).Where("family_id = ? AND actor_parent_id IS NULL", familyID).Count(&auditAfter).Error; err != nil {
				t.Fatal(err)
			}
			if self {
				auditBefore++
			}
			if auditAfter != auditBefore {
				t.Errorf("retained audit count = %d, want %d", auditAfter, auditBefore)
			}
			var deletion AuditEvent
			if err := db.Where("family_id = ? AND action = ? AND target_id = ?", familyID, "parent.delete", parent.ID.String()).First(&deletion).Error; err != nil {
				t.Fatal(err)
			}
			if self && deletion.ActorParentID != nil || !self && (deletion.ActorParentID == nil || *deletion.ActorParentID != admin.ID) {
				t.Errorf("deletion audit actor = %v", deletion.ActorParentID)
			}
		})
	}
}

func TestDeleteParentProtectsFamilyBoundaryAndLastAdmin(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	accounts := NewAccounts(db, nil)
	familyID := newFamily(t, db)
	otherFamilyID := newFamily(t, db)
	parent, err := accounts.CreateParent(ctx, familyID, uuid.NewString()+"@example.com", "hash", domain.RoleAdmin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.DeleteParent(ctx, otherFamilyID, parent.ID, parent.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-family deletion = %v, want not found", err)
	}
	if err := accounts.DeleteParent(ctx, familyID, parent.ID, parent.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("last admin deletion = %v, want last admin", err)
	}
	if _, err := accounts.Parent(ctx, parent.ID); err != nil {
		t.Errorf("protected parent missing: %v", err)
	}
	var count int64
	if err := db.Model(&AuditEvent{}).Where("target_id = ? AND action = ?", parent.ID.String(), "parent.delete").Count(&count).Error; err != nil || count != 0 {
		t.Errorf("failed deletion audit count = %d, error = %v", count, err)
	}
}
