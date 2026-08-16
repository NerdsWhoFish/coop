package auth

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

func rfcTOTPSecret() string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString([]byte("12345678901234567890"))
}

func TestMatchTOTPStepRFCVector(t *testing.T) {
	now := time.Unix(59, 0)
	step, ok := MatchTOTPStep("287082", rfcTOTPSecret(), now)
	if !ok {
		t.Fatal("MatchTOTPStep() rejected the RFC 6238 SHA-1 vector")
	}
	if step != 1 {
		t.Errorf("step = %d, want 1", step)
	}
}

func TestMatchTOTPStepAcceptsClockSkew(t *testing.T) {
	step, ok := MatchTOTPStep("287082", rfcTOTPSecret(), time.Unix(89, 0))
	if !ok || step != 1 {
		t.Fatalf("MatchTOTPStep() = (%d, %v), want previous step 1", step, ok)
	}
}

func TestMatchTOTPStepRejectsMalformedCode(t *testing.T) {
	for _, code := range []string{"", "12345", "1234567", "12345a"} {
		if _, ok := MatchTOTPStep(code, "secret", time.Now()); ok {
			t.Errorf("MatchTOTPStep(%q) accepted malformed code", code)
		}
	}
}

func TestNewTOTPSecret(t *testing.T) {
	secret, provisioningURL, err := NewTOTPSecret("parent@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" {
		t.Fatal("NewTOTPSecret() returned an empty secret")
	}
	if !strings.HasPrefix(provisioningURL, "otpauth://totp/Coop:") {
		t.Errorf("provisioning URL = %q, want Coop TOTP URL", provisioningURL)
	}
}
