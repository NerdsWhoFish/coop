//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

var rosterTestNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

type roster struct {
	accounts *Accounts
	family   Family
	admin    Parent
	child    Child
	device   ChildDevice
	session  ParentSession
	ctx      context.Context
}

func newRoster(t *testing.T) roster {
	t.Helper()
	db := migratedDB(t)
	accounts := NewAccounts(db, fixedClock(rosterTestNow))
	ctx := context.Background()

	family, admin, err := accounts.CreateFamily(
		ctx, "Roster Test", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&Family{}, "id = ?", family.ID) })

	child, err := accounts.CreateChild(ctx, family.ID, "Cooper", "", admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	code := uuid.NewString()
	if err := accounts.CreatePairingCode(ctx, child.ID, code,
		rosterTestNow.Add(time.Hour), admin.ID); err != nil {
		t.Fatal(err)
	}
	_, device, err := accounts.RedeemPairingCode(ctx, code, "Cooper's iPad", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}

	session, err := accounts.CreateSession(ctx, admin.ID, uuid.NewString(),
		rosterTestNow.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	return roster{accounts, family, admin, child, device, session, ctx}
}

func TestTouchRecordsTheReportedBuild(t *testing.T) {
	r := newRoster(t)
	reported := ClientApp{Build: "10800", Version: "1.8.0"}

	if err := r.accounts.TouchSession(r.ctx, r.session.ID, reported); err != nil {
		t.Fatal(err)
	}
	if err := r.accounts.TouchDevice(r.ctx, r.device.ID, reported); err != nil {
		t.Fatal(err)
	}

	for _, device := range r.devices(t) {
		if device.AppBuild != "10800" || device.AppVersion != "1.8.0" {
			t.Errorf("%s: build %q version %q, want 10800/1.8.0",
				device.Audience, device.AppBuild, device.AppVersion)
		}
	}
}

// A client that reports nothing must not erase a build already recorded, or a
// browser session would make a native device look like it had never updated.
func TestTouchWithoutAReportKeepsTheKnownBuild(t *testing.T) {
	r := newRoster(t)

	if err := r.accounts.TouchDevice(r.ctx, r.device.ID,
		ClientApp{Build: "22", Version: "0.1.0"}); err != nil {
		t.Fatal(err)
	}
	if err := r.accounts.TouchDevice(r.ctx, r.device.ID, ClientApp{}); err != nil {
		t.Fatal(err)
	}

	device := r.find(t, "child")
	if device.AppBuild != "22" {
		t.Errorf("AppBuild = %q after a silent client, want the known 22", device.AppBuild)
	}
}

func TestFamilyDevicesCoversBothAudiences(t *testing.T) {
	r := newRoster(t)

	devices := r.devices(t)
	if len(devices) != 2 {
		t.Fatalf("FamilyDevices() returned %d rows, want a parent session and a child device", len(devices))
	}

	parent := r.find(t, "parent")
	if parent.Owner != r.admin.Email {
		t.Errorf("parent owner = %q, want %q", parent.Owner, r.admin.Email)
	}
	if parent.ID != r.session.ID {
		t.Errorf("parent row is %v, want the session %v", parent.ID, r.session.ID)
	}

	child := r.find(t, "child")
	if child.Owner != "Cooper" || child.Name != "Cooper's iPad" {
		t.Errorf("child row = owner %q name %q, want Cooper / Cooper's iPad", child.Owner, child.Name)
	}

	// Nobody has reported yet, which is what an unmigrated family looks like.
	for _, device := range devices {
		if device.AppBuild != "" {
			t.Errorf("%s reported %q before any client said anything", device.Audience, device.AppBuild)
		}
	}
}

// A revoked device is not something a parent still has to migrate.
func TestFamilyDevicesOmitsRevokedDevices(t *testing.T) {
	r := newRoster(t)

	if err := r.accounts.RevokeDevice(r.ctx, r.device.ID, r.admin.ID); err != nil {
		t.Fatal(err)
	}
	for _, device := range r.devices(t) {
		if device.Audience == "child" {
			t.Error("FamilyDevices() still lists a revoked device")
		}
	}
}

func TestFamilyDevicesOmitsExpiredSessions(t *testing.T) {
	r := newRoster(t)

	expired := NewAccounts(r.accounts.db, fixedClock(rosterTestNow.Add(48*time.Hour)))
	devices, err := expired.FamilyDevices(r.ctx, r.family.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, device := range devices {
		if device.Audience == "parent" {
			t.Error("FamilyDevices() still lists a signed-out session")
		}
	}
}

func (r roster) devices(t *testing.T) []FamilyDevice {
	t.Helper()
	devices, err := r.accounts.FamilyDevices(r.ctx, r.family.ID)
	if err != nil {
		t.Fatal(err)
	}
	return devices
}

func (r roster) find(t *testing.T, audience string) FamilyDevice {
	t.Helper()
	for _, device := range r.devices(t) {
		if device.Audience == audience {
			return device
		}
	}
	t.Fatalf("no %s row in the roster", audience)
	return FamilyDevice{}
}
