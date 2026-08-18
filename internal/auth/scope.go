package auth

import (
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
)

// ErrOutOfScope reports a child the caller may not act on. Handlers render it
// as 404, never 403: a 403 confirms the child exists, which is precisely what
// scoping withholds.
var ErrOutOfScope = errors.New("auth: child is outside the caller's scope")

// ErrNotAdmin reports an admin-only action attempted by a scoped parent.
var ErrNotAdmin = errors.New("auth: action requires an admin parent")

// Parent is an authenticated adult and the scope they act within.
type Parent struct {
	ID       uuid.UUID
	FamilyID uuid.UUID
	Role     domain.ParentRole

	// SessionID is the sign-in this request arrived on, so a listing can say
	// which row is the caller's own device.
	SessionID uuid.UUID

	// ScopedChildIDs lists the children a non-admin may act on. It is ignored
	// for admins, who see the whole family.
	ScopedChildIDs []uuid.UUID
}

// IsAdmin reports whether this parent manages the family itself.
func (p Parent) IsAdmin() bool { return p.Role == domain.RoleAdmin }

// RequireAdmin returns ErrNotAdmin unless this parent is an admin.
func (p Parent) RequireAdmin() error {
	if !p.IsAdmin() {
		return ErrNotAdmin
	}
	return nil
}

// CanManage reports whether this parent may act on a child.
func (p Parent) CanManage(childID uuid.UUID) bool {
	if p.IsAdmin() {
		return true
	}
	return slices.Contains(p.ScopedChildIDs, childID)
}

// RequireChild returns ErrOutOfScope unless this parent may act on the child.
// Prefer it over CanManage at call sites: an ignored error is a louder bug
// than an ignored bool, and the silent failure here serves another kid's data.
func (p Parent) RequireChild(childID uuid.UUID) error {
	if !p.CanManage(childID) {
		return ErrOutOfScope
	}
	return nil
}

// FilterChildren narrows a family-wide list to what this parent may see.
func (p Parent) FilterChildren(childIDs []uuid.UUID) []uuid.UUID {
	if p.IsAdmin() {
		return slices.Clone(childIDs)
	}
	out := make([]uuid.UUID, 0, len(childIDs))
	for _, id := range childIDs {
		if slices.Contains(p.ScopedChildIDs, id) {
			out = append(out, id)
		}
	}
	return out
}

// Child is an authenticated child device and the profile it is bound to.
// Deliberately no role: the child surface is read-mostly and uniform, so there
// is nothing for a role to vary.
type Child struct {
	ID       uuid.UUID
	FamilyID uuid.UUID
	DeviceID uuid.UUID
}
