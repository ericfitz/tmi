package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

// generateTestKey generates a random 32-byte key for testing.
func generateTestKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}
	return key
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := generateTestKey(t)
	enc, err := NewSettingsEncryptorFromKeys(key, nil, 1)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	plaintext := "hello world, this is a test setting value!"
	encrypted, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if !IsEncrypted(encrypted) {
		t.Errorf("encrypted value should have ENC: prefix, got: %s", encrypted[:20])
	}

	decrypted, err := enc.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("round-trip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptPlaintextPassthrough(t *testing.T) {
	key := generateTestKey(t)
	enc, err := NewSettingsEncryptorFromKeys(key, nil, 1)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	// Non-encrypted values should pass through unchanged
	values := []string{
		"100",
		"true",
		"auto",
		`{"key": "value"}`,
		"",
		"some plain text",
	}

	for _, val := range values {
		result, err := enc.Decrypt(val)
		if err != nil {
			t.Errorf("Decrypt(%q) returned error: %v", val, err)
		}
		if result != val {
			t.Errorf("Decrypt(%q) = %q, want passthrough", val, result)
		}
	}
}

func TestEncryptDisabled(t *testing.T) {
	enc := &SettingsEncryptor{enabled: false}

	plaintext := "some value"
	result, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if result != plaintext {
		t.Errorf("disabled Encrypt should return plaintext, got %q", result)
	}
}

func TestUniqueNonces(t *testing.T) {
	key := generateTestKey(t)
	enc, err := NewSettingsEncryptorFromKeys(key, nil, 1)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	plaintext := "same value"
	encrypted1, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 1 failed: %v", err)
	}
	encrypted2, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 2 failed: %v", err)
	}

	if encrypted1 == encrypted2 {
		t.Error("encrypting the same value twice should produce different ciphertext (unique nonces)")
	}

	// Both should decrypt to the same value
	d1, _ := enc.Decrypt(encrypted1)
	d2, _ := enc.Decrypt(encrypted2)
	if d1 != plaintext || d2 != plaintext {
		t.Errorf("both ciphertexts should decrypt to %q, got %q and %q", plaintext, d1, d2)
	}
}

func TestWrongKeyFails(t *testing.T) {
	key1 := generateTestKey(t)
	key2 := generateTestKey(t)

	enc1, _ := NewSettingsEncryptorFromKeys(key1, nil, 1)
	enc2, _ := NewSettingsEncryptorFromKeys(key2, nil, 1)

	encrypted, err := enc1.Encrypt("secret data")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = enc2.Decrypt(encrypted)
	if err == nil {
		t.Error("Decrypt with wrong key should fail")
	}
}

func TestDecryptWithPreviousKey(t *testing.T) {
	keyA := generateTestKey(t)
	keyB := generateTestKey(t)

	// Encrypt with key A
	encA, _ := NewSettingsEncryptorFromKeys(keyA, nil, 1)
	encrypted, err := encA.Encrypt("secret data")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Decrypt with key B as current, key A as previous
	encB, err := NewSettingsEncryptorFromKeys(keyB, keyA, 2)
	if err != nil {
		t.Fatalf("failed to create encryptor with previous key: %v", err)
	}

	decrypted, err := encB.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt with previous key should succeed: %v", err)
	}
	if decrypted != "secret data" {
		t.Errorf("got %q, want %q", decrypted, "secret data")
	}
}

func TestDecryptFailsBothKeys(t *testing.T) {
	keyA := generateTestKey(t)
	keyB := generateTestKey(t)
	keyC := generateTestKey(t)

	// Encrypt with key A
	encA, _ := NewSettingsEncryptorFromKeys(keyA, nil, 1)
	encrypted, err := encA.Encrypt("secret data")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Try to decrypt with key B (current) and key C (previous) — neither is key A
	encBC, _ := NewSettingsEncryptorFromKeys(keyB, keyC, 2)
	_, err = encBC.Decrypt(encrypted)
	if err == nil {
		t.Error("Decrypt should fail when neither current nor previous key matches")
	}
}

func TestCorruptedCiphertext(t *testing.T) {
	key := generateTestKey(t)
	enc, _ := NewSettingsEncryptorFromKeys(key, nil, 1)

	encrypted, _ := enc.Encrypt("test value")

	// Corrupt the base64 data portion
	parts := strings.SplitN(encrypted, ":", 5)
	// Flip a character in the base64 data
	data := []byte(parts[4])
	if len(data) > 5 {
		data[5] ^= 0xFF
	}
	parts[4] = string(data)
	corrupted := strings.Join(parts, ":")

	_, err := enc.Decrypt(corrupted)
	if err == nil {
		t.Error("Decrypt of corrupted ciphertext should fail")
	}
}

func TestMalformedFormat(t *testing.T) {
	key := generateTestKey(t)
	enc, _ := NewSettingsEncryptorFromKeys(key, nil, 1)

	cases := []string{
		"ENC:",                // too few parts
		"ENC:v1",              // too few parts
		"ENC:v1:1",            // too few parts
		"ENC:v1:1:123",        // too few parts
		"ENC:v2:1:123:data",   // wrong version
		"ENC:v1:1:123:!!!###", // invalid base64 data
	}

	for _, c := range cases {
		_, err := enc.Decrypt(c)
		if err == nil {
			t.Errorf("Decrypt(%q) should return error for malformed format", c)
		}
	}
}

func TestInvalidKeyLength(t *testing.T) {
	// Too short
	_, err := NewSettingsEncryptorFromKeys(make([]byte, 16), nil, 1)
	if err == nil {
		t.Error("should reject 16-byte key")
	}

	// Too long
	_, err = NewSettingsEncryptorFromKeys(make([]byte, 64), nil, 1)
	if err == nil {
		t.Error("should reject 64-byte key")
	}

	// Invalid previous key length
	validKey := generateTestKey(t)
	_, err = NewSettingsEncryptorFromKeys(validKey, make([]byte, 16), 1)
	if err == nil {
		t.Error("should reject 16-byte previous key")
	}
}

func TestValueTooLong(t *testing.T) {
	key := generateTestKey(t)
	enc, _ := NewSettingsEncryptorFromKeys(key, nil, 1)

	// Create a value that will exceed 4000 chars when encrypted
	// Base64 expansion is 4/3, plus ~25 char prefix, plus 28 bytes AES-GCM overhead
	// So a plaintext of 3000 chars should push past the limit
	longValue := strings.Repeat("x", 3000)
	_, err := enc.Encrypt(longValue)
	if err == nil {
		t.Error("should reject value that exceeds varchar(4000) when encrypted")
	}
	if err != nil && !strings.Contains(err.Error(), "exceeds maximum storage length") {
		t.Errorf("expected ErrValueTooLong, got: %v", err)
	}
}

func TestContextIDInOutput(t *testing.T) {
	key := generateTestKey(t)

	for _, contextID := range []int{1, 5, 42} {
		enc, _ := NewSettingsEncryptorFromKeys(key, nil, contextID)
		encrypted, err := enc.Encrypt("test")
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}

		parts := strings.SplitN(encrypted, ":", 5)
		if len(parts) != 5 {
			t.Fatalf("expected 5 parts, got %d", len(parts))
		}

		gotID := parts[2]
		wantID := strconv.Itoa(contextID)
		if gotID != wantID {
			t.Errorf("context ID: got %s, want %s", gotID, wantID)
		}
	}
}

func TestIsEncrypted(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"ENC:v1:1:123:data", true},
		{"ENC:", true},
		{"ENC:anything", true},
		{"enc:v1:1:123:data", false}, // case sensitive
		{"ENCRYPTED:v1:1:123:data", false},
		{"100", false},
		{"true", false},
		{"", false},
		{"hello world", false},
	}

	for _, tc := range cases {
		got := IsEncrypted(tc.value)
		if got != tc.want {
			t.Errorf("IsEncrypted(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestHasPreviousKey(t *testing.T) {
	key := generateTestKey(t)

	enc1, _ := NewSettingsEncryptorFromKeys(key, nil, 1)
	if enc1.HasPreviousKey() {
		t.Error("should not have previous key")
	}

	prevKey := generateTestKey(t)
	enc2, _ := NewSettingsEncryptorFromKeys(key, prevKey, 1)
	if !enc2.HasPreviousKey() {
		t.Error("should have previous key")
	}
}

func TestIsEnabled(t *testing.T) {
	disabled := &SettingsEncryptor{enabled: false}
	if disabled.IsEnabled() {
		t.Error("disabled encryptor should return false")
	}

	key := generateTestKey(t)
	enabled, _ := NewSettingsEncryptorFromKeys(key, nil, 1)
	if !enabled.IsEnabled() {
		t.Error("enabled encryptor should return true")
	}
}

func TestGetContext(t *testing.T) {
	key := generateTestKey(t)
	enc, _ := NewSettingsEncryptorFromKeys(key, nil, 7)

	ctx := enc.GetContext()
	if ctx.ContextID != 7 {
		t.Errorf("ContextID: got %d, want 7", ctx.ContextID)
	}
	if ctx.Algorithm != "aes-256-gcm" {
		t.Errorf("Algorithm: got %s, want aes-256-gcm", ctx.Algorithm)
	}
}

func TestDecodeHexKey(t *testing.T) {
	// Valid key
	validHex := hex.EncodeToString(generateTestKey(t))
	key, err := decodeHexKey(validHex)
	if err != nil {
		t.Fatalf("valid hex key should decode: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length: got %d, want 32", len(key))
	}

	// With whitespace
	key2, err := decodeHexKey("  " + validHex + "  ")
	if err != nil {
		t.Fatalf("hex key with whitespace should decode: %v", err)
	}
	if len(key2) != 32 {
		t.Errorf("key length: got %d, want 32", len(key2))
	}

	// Invalid hex
	_, err = decodeHexKey("not-hex")
	if err == nil {
		t.Error("invalid hex should fail")
	}

	// Wrong length (16 bytes = 32 hex chars)
	shortHex := hex.EncodeToString(make([]byte, 16))
	_, err = decodeHexKey(shortHex)
	if err == nil {
		t.Error("16-byte key should fail")
	}
}

func TestEmptyPlaintext(t *testing.T) {
	key := generateTestKey(t)
	enc, _ := NewSettingsEncryptorFromKeys(key, nil, 1)

	encrypted, err := enc.Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt empty string failed: %v", err)
	}

	decrypted, err := enc.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt empty string failed: %v", err)
	}
	if decrypted != "" {
		t.Errorf("got %q, want empty string", decrypted)
	}
}
