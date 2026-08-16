package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
)

const goodPassword = "correct horse battery staple"

func TestHashPasswordRejectsShortPasswords(t *testing.T) {
	for _, weak := range []string{"", "short", strings.Repeat("a", MinPasswordLength-1)} {
		if _, err := HashPassword(weak); !errors.Is(err, ErrWeakPassword) {
			t.Errorf("HashPassword(%q) error = %v, want ErrWeakPassword", weak, err)
		}
	}
}

// Length is counted in runes, so a short password of wide characters is still
// rejected and a long one of them is still accepted.
func TestPasswordLengthCountsRunes(t *testing.T) {
	if _, err := HashPassword(strings.Repeat("é", MinPasswordLength-1)); !errors.Is(err, ErrWeakPassword) {
		t.Error("a short unicode password was accepted")
	}
	if _, err := HashPassword(strings.Repeat("é", MinPasswordLength)); err != nil {
		t.Errorf("a long enough unicode password was rejected: %v", err)
	}
}

func TestVerifyPassword(t *testing.T) {
	hash, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := VerifyPassword(hash, goodPassword)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !ok {
		t.Error("the correct password did not verify")
	}

	ok, err = VerifyPassword(hash, goodPassword+"x")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if ok {
		t.Error("a wrong password verified")
	}
}

// Every hash carries its own salt, so the same password must never produce the
// same stored value twice.
func TestHashesAreSalted(t *testing.T) {
	first, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("two hashes of the same password are identical, want distinct salts")
	}
}

func TestHashEncodesItsParameters(t *testing.T) {
	hash, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	// Parameters travel with the hash so they can be raised without a migration.
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Errorf("hash = %q, want the argon2id encoded form with parameters", hash)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	tests := []string{
		"",
		"not a hash",
		"$argon2id$v=19$m=65536,t=3,p=2$onlyfourparts",
		"$bcrypt$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=99$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=19$garbage$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=2$!!!notbase64$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=2$$",
	}
	for _, hash := range tests {
		t.Run(hash, func(t *testing.T) {
			ok, err := VerifyPassword(hash, goodPassword)
			if err == nil {
				t.Error("VerifyPassword() error = nil, want a parse failure")
			}
			if ok {
				t.Error("VerifyPassword() = true for a malformed hash")
			}
		})
	}
}

// Timing must not reveal whether an account exists, so the equalizer has to be
// a real hash that costs the same work as a genuine verification.
func TestSpendVerifyTimeDoesNotPanic(t *testing.T) {
	SpendVerifyTime()
}

func TestNewToken(t *testing.T) {
	first, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}

	if first.Plain == second.Plain {
		t.Error("two tokens are identical")
	}
	if first.Hash != HashToken(first.Plain) {
		t.Error("Token.Hash does not match HashToken(Plain)")
	}
	// The plaintext must never be recoverable from what gets persisted.
	if strings.Contains(first.Hash, first.Plain) {
		t.Error("the stored hash contains the plaintext token")
	}
	if len(first.Plain) < 40 {
		t.Errorf("token length = %d, want at least 40 encoded characters", len(first.Plain))
	}
}

func TestEqualTokenHash(t *testing.T) {
	if !EqualTokenHash("abc", "abc") {
		t.Error("identical hashes did not compare equal")
	}
	if EqualTokenHash("abc", "abd") {
		t.Error("different hashes compared equal")
	}
	if EqualTokenHash("abc", "") {
		t.Error("a hash compared equal to the empty string")
	}
}

func TestBearerToken(t *testing.T) {
	tests := []struct{ header, want string }{
		{header: "Bearer abc123", want: "abc123"},
		{header: "bearer abc123", want: "abc123"},
		{header: "BEARER abc123", want: "abc123"},
		{header: "Bearer   abc123  ", want: "abc123"},
		{header: "", want: ""},
		{header: "Bearer", want: ""},
		{header: "Bearer ", want: ""},
		{header: "Basic abc123", want: ""},
		{header: "abc123", want: ""},
	}
	for _, tt := range tests {
		if got := BearerToken(tt.header); got != tt.want {
			t.Errorf("BearerToken(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestNewPairingCode(t *testing.T) {
	code, err := NewPairingCode()
	if err != nil {
		t.Fatal(err)
	}

	if len(code) != pairingGroups*pairingGroupSize+pairingGroups-1 {
		t.Errorf("code = %q, want the grouped form", code)
	}
	if !strings.Contains(code, "-") {
		t.Errorf("code = %q, want group separators", code)
	}

	// Ambiguous characters would turn a read-aloud code into a support call.
	for _, r := range strings.ReplaceAll(code, "-", "") {
		if !strings.ContainsRune(pairingAlphabet, r) {
			t.Errorf("code %q contains %q, which is outside the unambiguous alphabet", code, r)
		}
	}
}

func TestNormalizePairingCode(t *testing.T) {
	tests := []struct{ in, want string }{
		{in: "ACDE-FGHJ", want: "ACDE-FGHJ"},
		{in: "acde-fghj", want: "ACDE-FGHJ"},
		{in: "acdefghj", want: "ACDE-FGHJ"},
		{in: " ACDE FGHJ ", want: "ACDE-FGHJ"},
		{in: "ACDE_FGHJ", want: "ACDE-FGHJ"},
		// Too short, too long, and characters outside the alphabet all fail
		// closed rather than producing a code that nearly works.
		{in: "ACDE-FGH", want: ""},
		{in: "ACDE-FGHJK", want: ""},
		{in: "", want: ""},
		{in: "0OIL1258", want: ""},
	}
	for _, tt := range tests {
		if got := NormalizePairingCode(tt.in); got != tt.want {
			t.Errorf("NormalizePairingCode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPairingCodesNormalizeToThemselves(t *testing.T) {
	for range 50 {
		code, err := NewPairingCode()
		if err != nil {
			t.Fatal(err)
		}
		if got := NormalizePairingCode(code); got != code {
			t.Fatalf("NormalizePairingCode(%q) = %q, want it unchanged", code, got)
		}
	}
}

func TestParentScoping(t *testing.T) {
	alice := uuid.New()
	bob := uuid.New()
	carol := uuid.New()

	admin := Parent{Role: domain.RoleAdmin}
	scoped := Parent{Role: domain.RoleParent, ScopedChildIDs: []uuid.UUID{alice, bob}}
	unscoped := Parent{Role: domain.RoleParent}

	t.Run("admin manages every child", func(t *testing.T) {
		for _, id := range []uuid.UUID{alice, bob, carol} {
			if !admin.CanManage(id) {
				t.Errorf("admin cannot manage %s", id)
			}
			if err := admin.RequireChild(id); err != nil {
				t.Errorf("admin.RequireChild(%s) = %v", id, err)
			}
		}
	})

	t.Run("scoped parent manages only their children", func(t *testing.T) {
		if !scoped.CanManage(alice) || !scoped.CanManage(bob) {
			t.Error("scoped parent cannot manage a child in scope")
		}
		if scoped.CanManage(carol) {
			t.Error("scoped parent can manage a child outside scope")
		}
		if err := scoped.RequireChild(carol); !errors.Is(err, ErrOutOfScope) {
			t.Errorf("RequireChild(out of scope) = %v, want ErrOutOfScope", err)
		}
	})

	t.Run("a parent with no scope manages nobody", func(t *testing.T) {
		for _, id := range []uuid.UUID{alice, bob, carol} {
			if unscoped.CanManage(id) {
				t.Errorf("unscoped parent can manage %s", id)
			}
		}
	})

	t.Run("admin only actions", func(t *testing.T) {
		if err := admin.RequireAdmin(); err != nil {
			t.Errorf("admin.RequireAdmin() = %v", err)
		}
		if err := scoped.RequireAdmin(); !errors.Is(err, ErrNotAdmin) {
			t.Errorf("scoped.RequireAdmin() = %v, want ErrNotAdmin", err)
		}
	})
}

func TestFilterChildren(t *testing.T) {
	alice := uuid.New()
	bob := uuid.New()
	carol := uuid.New()
	all := []uuid.UUID{alice, bob, carol}

	admin := Parent{Role: domain.RoleAdmin}
	if got := admin.FilterChildren(all); len(got) != 3 {
		t.Errorf("admin sees %d children, want all 3", len(got))
	}

	scoped := Parent{Role: domain.RoleParent, ScopedChildIDs: []uuid.UUID{bob}}
	got := scoped.FilterChildren(all)
	if len(got) != 1 || got[0] != bob {
		t.Errorf("scoped parent sees %v, want just bob", got)
	}

	empty := Parent{Role: domain.RoleParent}
	if got := empty.FilterChildren(all); len(got) != 0 {
		t.Errorf("unscoped parent sees %v, want nothing", got)
	}
}

// FilterChildren must not hand back the caller's slice, or a later mutation
// would silently change what a parent is allowed to see.
func TestFilterChildrenDoesNotAliasItsInput(t *testing.T) {
	alice := uuid.New()
	all := []uuid.UUID{alice}

	got := Parent{Role: domain.RoleAdmin}.FilterChildren(all)
	got[0] = uuid.New()

	if all[0] != alice {
		t.Error("FilterChildren returned a slice aliasing its input")
	}
}
