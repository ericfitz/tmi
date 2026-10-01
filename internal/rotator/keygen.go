package rotator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// NewPassword returns a 32-character alphanumeric password (URL- and shell-safe,
// so it needs no escaping in TMI_DATABASE_URL or redis-cli). int(b)%62 biases
// the first 8 letters by 4/256; acceptable for ~190 bits and simpler than
// rejection sampling.
// SEM@e9ba68231ad8e8bb838e0131e284b77148d8e5c1: generate a random 32-char alphanumeric credential (pure)
// SEM@e9ba68231ad8e8bb838e0131e284b77148d8e5c1: generate a random password (reads random source)
func NewPassword() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("random source failed: %w", err)
	}
	out := make([]byte, len(buf))
	for i, b := range buf {
		out[i] = passwordAlphabet[int(b)%len(passwordAlphabet)]
	}
	return string(out), nil
}

// NewHexKey returns 32 random bytes as 64 hex characters (AES-256 key).
// SEM@e9ba68231ad8e8bb838e0131e284b77148d8e5c1: generate a random 32-byte key as hex text (pure)
// SEM@e9ba68231ad8e8bb838e0131e284b77148d8e5c1: generate a random 256-bit key as hex (reads random source)
func NewHexKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("random source failed: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
