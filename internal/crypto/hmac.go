package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// GenerateHMACSignature generates an HMAC-SHA256 signature for the payload.
// Returns the signature in the format "sha256=<hex-encoded-mac>".
// SEM@54a00be20c0e4ff2748aa85a498fbebd3f2e9e37: compute a SHA-256 HMAC signature over a payload and return it as a sha256= prefixed hex string (pure)
func GenerateHMACSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyHMACSignature verifies an HMAC-SHA256 signature using constant-time comparison.
// SEM@54a00be20c0e4ff2748aa85a498fbebd3f2e9e37: validate an HMAC signature against a payload and secret using constant-time comparison (pure)
func VerifyHMACSignature(payload []byte, signature string, secret string) bool {
	expected := GenerateHMACSignature(payload, secret)
	return hmac.Equal([]byte(signature), []byte(expected))
}
