package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateHMACSignature(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		secret  string
	}{
		{"normal payload", []byte(`{"event":"test"}`), "mysecret"},
		{"empty payload", []byte{}, "mysecret"},
		{"empty secret", []byte("data"), ""},
		{"unicode payload", []byte("héllo wörld"), "secret123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := GenerateHMACSignature(tt.payload, tt.secret)
			// Must start with sha256= prefix
			assert.True(t, len(sig) > 7, "signature should not be empty")
			assert.Equal(t, "sha256=", sig[:7], "signature must start with sha256= prefix")
			// Hex-encoded SHA256 is 64 chars
			assert.Equal(t, 7+64, len(sig), "signature should be sha256= + 64 hex chars")
		})
	}
}

func TestGenerateHMACSignature_Deterministic(t *testing.T) {
	payload := []byte(`{"event":"webhook.delivered","id":"123"}`)
	secret := "webhook-secret-key"

	sig1 := GenerateHMACSignature(payload, secret)
	sig2 := GenerateHMACSignature(payload, secret)
	assert.Equal(t, sig1, sig2, "same inputs must produce same signature")
}

func TestGenerateHMACSignature_DifferentSecrets(t *testing.T) {
	payload := []byte(`{"event":"test"}`)

	sig1 := GenerateHMACSignature(payload, "secret1")
	sig2 := GenerateHMACSignature(payload, "secret2")
	assert.NotEqual(t, sig1, sig2, "different secrets must produce different signatures")
}

func TestVerifyHMACSignature(t *testing.T) {
	payload := []byte(`{"event":"webhook.delivered"}`)
	secret := "test-secret"

	// Generate a valid signature
	validSig := GenerateHMACSignature(payload, secret)

	tests := []struct {
		name      string
		payload   []byte
		signature string
		secret    string
		expected  bool
	}{
		{"valid signature", payload, validSig, secret, true},
		{"wrong secret", payload, validSig, "wrong-secret", false},
		{"wrong payload", []byte("tampered"), validSig, secret, false},
		{"empty signature", payload, "", secret, false},
		{"garbage signature", payload, "sha256=deadbeef", secret, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, VerifyHMACSignature(tt.payload, tt.signature, tt.secret))
		})
	}
}
