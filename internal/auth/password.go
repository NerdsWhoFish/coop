// Package auth handles parent credentials, device tokens, pairing codes, and
// the scoping rules that decide which children a parent may act on.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// MinPasswordLength is the shortest password accepted. Length beats
// composition rules, which mostly produce predictable substitutions.
const MinPasswordLength = 12

type argonParams struct {
	memoryKiB uint32
	passes    uint32
	threads   uint8
	keyLen    uint32
	saltLen   uint32
}

var defaultParams = argonParams{
	memoryKiB: 64 * 1024,
	passes:    3,
	threads:   2,
	keyLen:    32,
	saltLen:   16,
}

// ErrWeakPassword reports a password below MinPasswordLength.
var ErrWeakPassword = fmt.Errorf("password must be at least %d characters", MinPasswordLength)

// ErrBadHash reports an encoded hash this build cannot parse.
var ErrBadHash = errors.New("auth: unparseable password hash")

// HashPassword derives an argon2id hash in the standard encoded form, which
// carries its own parameters so they can be raised later without a migration.
func HashPassword(plain string) (string, error) {
	if len([]rune(plain)) < MinPasswordLength {
		return "", ErrWeakPassword
	}

	p := defaultParams
	salt := make([]byte, p.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: reading salt: %w", err)
	}

	key := argon2.IDKey([]byte(plain), salt, p.passes, p.memoryKiB, p.threads, p.keyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memoryKiB, p.passes, p.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword compares plain against an encoded hash in constant time.
func VerifyPassword(encoded, plain string) (bool, error) {
	p, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(plain), salt, p.passes, p.memoryKiB, p.threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is a real hash of a value nobody knows, used to spend the same
// work on a missing account as on a real one. Without it, response timing
// tells an attacker which email addresses exist.
var dummyHash = mustHash("coop-timing-equalizer-value")

// SpendVerifyTime performs a throwaway verification so an unknown account
// costs the same as a known one.
func SpendVerifyTime() {
	_, _ = VerifyPassword(dummyHash, "not-the-password")
}

func mustHash(plain string) string {
	h, err := HashPassword(plain)
	if err != nil {
		panic("auth: hashing the timing equalizer: " + err.Error())
	}
	return h
}

func decodeHash(encoded string) (p argonParams, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, ErrBadHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return p, nil, nil, ErrBadHash
	}
	if version != argon2.Version {
		return p, nil, nil, fmt.Errorf("%w: argon2 version %d", ErrBadHash, version)
	}

	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memoryKiB, &p.passes, &p.threads); err != nil {
		return p, nil, nil, ErrBadHash
	}

	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return p, nil, nil, ErrBadHash
	}
	if key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return p, nil, nil, ErrBadHash
	}
	if len(salt) == 0 || len(key) == 0 {
		return p, nil, nil, ErrBadHash
	}
	return p, salt, key, nil
}
