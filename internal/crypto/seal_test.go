package crypto

import (
	"bytes"
	"errors"
	"testing"
)

func testKey(b byte) []byte {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = b
	}
	return key
}

func TestNewSealerRejectsWrongKeySizes(t *testing.T) {
	for _, size := range []int{0, 1, 16, 31, 33, 64} {
		if _, err := NewSealer(make([]byte, size)); err == nil {
			t.Errorf("NewSealer(%d bytes) = nil error, want a rejection", size)
		}
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	s, err := NewSealer(testKey(1))
	if err != nil {
		t.Fatal(err)
	}

	for _, plaintext := range []string{"", "AIzaSyExampleKey", "a much longer secret with unicode: café 🐔"} {
		sealed, err := s.SealString(plaintext)
		if err != nil {
			t.Fatalf("SealString(%q) error = %v", plaintext, err)
		}
		got, err := s.OpenString(sealed)
		if err != nil {
			t.Fatalf("OpenString() error = %v", err)
		}
		if got != plaintext {
			t.Errorf("round trip = %q, want %q", got, plaintext)
		}
	}
}

// A repeated nonce would be catastrophic for GCM, so identical plaintext must
// never produce identical ciphertext.
func TestSealIsNotDeterministic(t *testing.T) {
	s, err := NewSealer(testKey(2))
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.SealString("same secret")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SealString("same secret")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Error("two seals of the same plaintext are byte-identical, want distinct nonces")
	}
}

func TestOpenRejectsTampering(t *testing.T) {
	s, err := NewSealer(testKey(3))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.SealString("secret")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		munge func([]byte) []byte
	}{
		{name: "flipped ciphertext bit", munge: func(b []byte) []byte {
			out := bytes.Clone(b)
			out[len(out)-1] ^= 0x01
			return out
		}},
		{name: "flipped nonce bit", munge: func(b []byte) []byte {
			out := bytes.Clone(b)
			out[0] ^= 0x01
			return out
		}},
		{name: "truncated", munge: func(b []byte) []byte { return b[:len(b)-1] }},
		{name: "shorter than a nonce", munge: func(b []byte) []byte { return b[:4] }},
		{name: "empty", munge: func([]byte) []byte { return nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Open(tt.munge(sealed)); !errors.Is(err, ErrCorrupt) {
				t.Errorf("Open() error = %v, want ErrCorrupt", err)
			}
		})
	}
}

// Losing the encryption key must fail loudly rather than return garbage.
func TestOpenRejectsAnotherKey(t *testing.T) {
	a, err := NewSealer(testKey(4))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSealer(testKey(5))
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := a.SealString("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Open(sealed); !errors.Is(err, ErrCorrupt) {
		t.Errorf("Open() with the wrong key = %v, want ErrCorrupt", err)
	}
}
