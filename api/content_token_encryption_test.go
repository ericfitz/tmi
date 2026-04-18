package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentTokenEncryption_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	enc, err := NewContentTokenEncryptor(hex.EncodeToString(key))
	require.NoError(t, err)

	plaintext := []byte("ya29.a0AfH6SMDe-example-access-token")
	ct, err := enc.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ct)

	pt, err := enc.Decrypt(ct)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(plaintext, pt))
}

func TestContentTokenEncryption_UniqueNonces(t *testing.T) {
	enc := mustNewTestEncryptor(t)
	pt := []byte("same-plaintext")
	ct1, err := enc.Encrypt(pt)
	require.NoError(t, err)
	ct2, err := enc.Encrypt(pt)
	require.NoError(t, err)
	assert.False(t, bytes.Equal(ct1, ct2), "identical plaintext must produce different ciphertext")
}

func TestContentTokenEncryption_RejectsShortKey(t *testing.T) {
	_, err := NewContentTokenEncryptor("deadbeef")
	assert.Error(t, err)
}

func TestContentTokenEncryption_RejectsInvalidHex(t *testing.T) {
	invalidHex := "ZZ" + strings.Repeat("00", 31)
	_, err := NewContentTokenEncryptor(invalidHex)
	assert.Error(t, err)
}

func TestContentTokenEncryption_TamperedCiphertextFailsDecrypt(t *testing.T) {
	enc := mustNewTestEncryptor(t)
	ct, err := enc.Encrypt([]byte("payload"))
	require.NoError(t, err)
	ct[len(ct)-1] ^= 0xFF
	_, err = enc.Decrypt(ct)
	assert.Error(t, err)
}

// mustNewTestEncryptor creates a ContentTokenEncryptor with a random key for use in tests.
// It is also used by repository tests in later tasks (do not remove).
func mustNewTestEncryptor(t *testing.T) *ContentTokenEncryptor {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	enc, err := NewContentTokenEncryptor(hex.EncodeToString(key))
	require.NoError(t, err)
	return enc
}
