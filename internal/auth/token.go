package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// tokenBytes is the entropy in a session or device token. 32 bytes is far past
// any brute-force concern and keeps the encoded form a manageable length.
const tokenBytes = 32

// Token is a freshly minted credential. Plain is shown to the caller exactly
// once; only Hash is ever persisted.
type Token struct {
	Plain string
	Hash  string
}

// NewToken mints a random bearer token.
func NewToken() (Token, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Token{}, fmt.Errorf("auth: reading token entropy: %w", err)
	}
	plain := base64.RawURLEncoding.EncodeToString(raw)
	return Token{Plain: plain, Hash: HashToken(plain)}, nil
}

// HashToken derives the stored form of a token. Plain SHA-256 is right where a
// password would need argon2: tokens carry full random entropy, so there is
// nothing to brute force, and this runs on every request.
func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// EqualTokenHash compares two stored hashes in constant time.
func EqualTokenHash(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// BearerToken extracts a token from an Authorization header, or returns "".
func BearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// pairingAlphabet drops characters confused when read aloud or typed on a
// child's device: 0/O, 1/I/L, 2/Z, 5/S, 8/B.
const pairingAlphabet = "ACDEFGHJKMNPQRTUVWXY34679"

// pairingGroups and pairingGroupSize give a code like ABCD-EFGH.
const (
	pairingGroups    = 2
	pairingGroupSize = 4
)

// NewPairingCode mints a short, single-use code for binding a child device.
// Its entropy is modest because it is typed by hand: safety comes from the
// short expiry and single use, not from the length.
func NewPairingCode() (string, error) {
	total := pairingGroups * pairingGroupSize
	raw := make([]byte, total)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: reading pairing entropy: %w", err)
	}

	var b strings.Builder
	for i, v := range raw {
		if i > 0 && i%pairingGroupSize == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(pairingAlphabet[int(v)%len(pairingAlphabet)])
	}
	return b.String(), nil
}

// NormalizePairingCode makes entry forgiving: case and separators do not
// matter, so a parent reading a code aloud cannot get it subtly wrong.
func NormalizePairingCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if strings.ContainsRune(pairingAlphabet, r) {
			b.WriteRune(r)
		}
	}

	out := b.String()
	if len(out) != pairingGroups*pairingGroupSize {
		return ""
	}

	var formatted strings.Builder
	for i := range len(out) {
		if i > 0 && i%pairingGroupSize == 0 {
			formatted.WriteByte('-')
		}
		formatted.WriteByte(out[i])
	}
	return formatted.String()
}
