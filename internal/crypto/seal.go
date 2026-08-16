// Package crypto seals the secrets Coop stores on a family's behalf: their
// YouTube API key and any TOTP secrets.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// Sealer encrypts and decrypts with AES-256-GCM.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer builds a Sealer from a 32 byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("crypto: key is %d bytes, want %d", len(key), KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: building cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: building GCM: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// ErrCorrupt reports ciphertext that failed authentication, which means it was
// truncated, tampered with, or encrypted under a different key.
var ErrCorrupt = errors.New("crypto: ciphertext failed authentication")

// Seal encrypts plaintext, returning nonce followed by ciphertext. The nonce
// must be fresh and random every call: GCM leaks its authentication key
// outright if one is ever reused under the same key.
func (s *Sealer) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto: reading nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open reverses Seal.
func (s *Sealer) Open(sealed []byte) ([]byte, error) {
	size := s.aead.NonceSize()
	if len(sealed) < size {
		return nil, ErrCorrupt
	}
	plaintext, err := s.aead.Open(nil, sealed[:size], sealed[size:], nil)
	if err != nil {
		return nil, ErrCorrupt
	}
	return plaintext, nil
}

// SealString is Seal for text secrets.
func (s *Sealer) SealString(plaintext string) ([]byte, error) {
	return s.Seal([]byte(plaintext))
}

// OpenString is Open for text secrets.
func (s *Sealer) OpenString(sealed []byte) (string, error) {
	plaintext, err := s.Open(sealed)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
