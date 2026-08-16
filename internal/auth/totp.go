package auth

import (
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const totpPeriod = int64(30)

// NewTOTPSecret creates a standard six-digit SHA-1 TOTP credential compatible
// with platform password managers and authenticator apps.
func NewTOTPSecret(account string) (secret, provisioningURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Coop",
		AccountName: account,
		Period:      uint(totpPeriod),
		SecretSize:  32,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", fmt.Errorf("auth: generating TOTP secret: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// MatchTOTPStep verifies a code and returns the exact accepted timestep.
// Returning the step lets persistence reject a code replay even when two
// requests arrive during the same 30-second window.
func MatchTOTPStep(code, secret string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return 0, false
		}
	}

	current := now.Unix() / totpPeriod
	for _, offset := range []int64{0, -1, 1} {
		step := current + offset
		candidate, err := totp.GenerateCodeCustom(secret, time.Unix(step*totpPeriod, 0), totp.ValidateOpts{
			Period:    uint(totpPeriod),
			Skew:      0,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err == nil && subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}
