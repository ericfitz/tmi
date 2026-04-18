package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// NewPKCEVerifier returns a 43-character code verifier per RFC 7636 §4.1.
// It is 32 random bytes encoded as base64url without padding.
func NewPKCEVerifier() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// PKCES256Challenge returns the S256 code challenge for the given verifier
// per RFC 7636 §4.2: BASE64URL(SHA256(ASCII(code_verifier))) without padding.
func PKCES256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
