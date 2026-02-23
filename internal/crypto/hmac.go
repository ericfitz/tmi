package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// GenerateHMACSignature generates an HMAC-SHA256 signature for the payload.
// Returns the signature in the format "sha256=<hex-encoded-mac>".
func GenerateHMACSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyHMACSignature verifies an HMAC-SHA256 signature using constant-time comparison.
func VerifyHMACSignature(payload []byte, signature string, secret string) bool {
	expected := GenerateHMACSignature(payload, secret)
	return hmac.Equal([]byte(signature), []byte(expected))
}
