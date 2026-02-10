package db

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testKey generates a random 32-byte AES-256 key for testing.
func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return key
}

// setupTestRedisDB creates a RedisDB backed by miniredis for testing.
func setupTestRedisDB(t *testing.T) (*RedisDB, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return &RedisDB{
		client: client,
		cfg:    RedisConfig{Host: mr.Host(), Port: mr.Port()},
	}, mr
}

// TestShouldEncrypt_SensitiveKeys verifies that all expected sensitive prefixes trigger encryption.
func TestShouldEncrypt_SensitiveKeys(t *testing.T) {
	sensitiveKeys := []string{
		"cache:user:550e8400-e29b-41d4-a716-446655440000",
		"cache:user:email:alice@example.com",
		"cache:user:provider:google:12345",
		"refresh_token:550e8400-e29b-41d4-a716-446655440000",
		"user_groups:alice@example.com",
		"tmi:settings:smtp.password",
		"oauth_state:abc123",
		"pkce:auth_code_123",
		"user_deletion_challenge:alice@example.com",
		"session:user-uuid:session-uuid",
		"auth:state:state123",
		"auth:refresh:refresh-uuid",
	}

	for _, key := range sensitiveKeys {
		assert.True(t, shouldEncrypt(key), "expected shouldEncrypt(%q) = true", key)
	}
}

// TestShouldEncrypt_NonSensitiveKeys verifies that non-sensitive keys do not trigger encryption.
func TestShouldEncrypt_NonSensitiveKeys(t *testing.T) {
	nonSensitiveKeys := []string{
		"blacklist:token:abc123hash",
		"rate_limit:global:192.168.1.1:api_users",
		"rate_limit:user:uuid:action",
		"lock:threat_model:uuid",
		"cache:threat_model:uuid",
		"cache:diagram:uuid",
		"cache:threat:uuid",
		"cache:document:uuid",
		"cache:note:uuid",
		"cache:repository:uuid",
		"cache:asset:uuid",
		"cache:metadata:type:uuid",
		"cache:cells:uuid",
		"cache:auth:uuid",
		"cache:list:type:parent:0:10",
		"temp:export:jobid",
		"temp:import:jobid",
		"tmi:events",
		"",
	}

	for _, key := range nonSensitiveKeys {
		assert.False(t, shouldEncrypt(key), "expected shouldEncrypt(%q) = false", key)
	}
}

// TestSetGet_EncryptsAndDecryptsSensitiveKey verifies round-trip encryption for sensitive keys.
func TestSetGet_EncryptsAndDecryptsSensitiveKey(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key := "cache:user:550e8400-e29b-41d4-a716-446655440000"
	plaintext := `{"email":"alice@example.com","name":"Alice"}`

	// Write encrypted value
	err = rdb.Set(ctx, key, plaintext, time.Minute)
	require.NoError(t, err)

	// Verify raw Redis value is encrypted (has ENC: prefix)
	rawValue, err := mr.Get(key)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(rawValue, "ENC:v1:"), "raw Redis value should be encrypted, got: %s", rawValue)
	assert.NotEqual(t, plaintext, rawValue)

	// Read through RedisDB should return plaintext
	result, err := rdb.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, result)
}

// TestSetGet_DoesNotEncryptNonSensitiveKey verifies non-sensitive keys are stored as plaintext.
func TestSetGet_DoesNotEncryptNonSensitiveKey(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key := "rate_limit:global:192.168.1.1:api_users"
	plaintext := "42"

	err = rdb.Set(ctx, key, plaintext, time.Minute)
	require.NoError(t, err)

	// Raw Redis value should be plaintext
	rawValue, err := mr.Get(key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, rawValue)

	// Read through RedisDB should also be plaintext
	result, err := rdb.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, result)
}

// TestSetGet_NoEncryptor verifies that without an encryptor, values pass through unchanged.
func TestSetGet_NoEncryptor(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	key := "cache:user:uuid-123"
	plaintext := `{"email":"bob@example.com"}`

	err := rdb.Set(ctx, key, plaintext, time.Minute)
	require.NoError(t, err)

	rawValue, err := mr.Get(key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, rawValue, "without encryptor, value should be plaintext")

	result, err := rdb.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, result)
}

// TestSetGet_DisabledEncryptor verifies that a disabled encryptor passes values through.
func TestSetGet_DisabledEncryptor(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	// Create a disabled encryptor (no key = disabled)
	enc := &crypto.SettingsEncryptor{}
	rdb.SetEncryptor(enc)

	key := "cache:user:uuid-456"
	plaintext := `{"email":"carol@example.com"}`

	err := rdb.Set(ctx, key, plaintext, time.Minute)
	require.NoError(t, err)

	rawValue, err := mr.Get(key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, rawValue, "disabled encryptor should store plaintext")

	result, err := rdb.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, result)
}

// TestGet_PlaintextPassthrough verifies that pre-existing plaintext values are returned as-is
// even when an encryptor is configured (supports rolling deployments).
func TestGet_PlaintextPassthrough(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key := "cache:user:uuid-789"
	plaintext := `{"email":"dave@example.com"}`

	// Write directly to miniredis (bypassing encryption), simulating pre-existing data
	require.NoError(t, mr.Set(key, plaintext))

	// Read through RedisDB should return plaintext (no ENC: prefix, so no decryption attempted)
	result, err := rdb.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, result)
}

// TestSetGet_UniqueNonces verifies that encrypting the same value twice produces different ciphertexts.
func TestSetGet_UniqueNonces(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key1 := "cache:user:uuid-a"
	key2 := "cache:user:uuid-b"
	plaintext := `{"email":"same@example.com"}`

	err = rdb.Set(ctx, key1, plaintext, time.Minute)
	require.NoError(t, err)
	err = rdb.Set(ctx, key2, plaintext, time.Minute)
	require.NoError(t, err)

	raw1, _ := mr.Get(key1)
	raw2, _ := mr.Get(key2)
	assert.NotEqual(t, raw1, raw2, "same plaintext should produce different ciphertexts due to random nonces")
}

// TestHSetHGet_EncryptsAndDecryptsSensitiveKey verifies hash operation encryption.
func TestHSetHGet_EncryptsAndDecryptsSensitiveKey(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key := "session:user-uuid:session-uuid"
	field := "user_data"
	plaintext := `{"email":"alice@example.com"}`

	err = rdb.HSet(ctx, key, field, plaintext)
	require.NoError(t, err)

	// Verify raw hash value is encrypted
	rawValue := mr.HGet(key, field)
	assert.True(t, strings.HasPrefix(rawValue, "ENC:v1:"), "raw hash value should be encrypted")

	// Read through RedisDB should return plaintext
	result, err := rdb.HGet(ctx, key, field)
	require.NoError(t, err)
	assert.Equal(t, plaintext, result)
}

// TestHSetHGet_DoesNotEncryptNonSensitiveKey verifies hash operations skip encryption for non-sensitive keys.
func TestHSetHGet_DoesNotEncryptNonSensitiveKey(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key := "lock:threat_model:uuid"
	field := "holder"
	plaintext := "worker-1"

	err = rdb.HSet(ctx, key, field, plaintext)
	require.NoError(t, err)

	rawValue := mr.HGet(key, field)
	assert.Equal(t, plaintext, rawValue, "non-sensitive hash value should be plaintext")

	result, err := rdb.HGet(ctx, key, field)
	require.NoError(t, err)
	assert.Equal(t, plaintext, result)
}

// TestHGetAll_DecryptsAllFields verifies HGetAll decrypts all encrypted fields.
func TestHGetAll_DecryptsAllFields(t *testing.T) {
	rdb, _ := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key := "session:user-uuid:session-uuid"

	// Set multiple fields
	err = rdb.HSet(ctx, key, "email", "alice@example.com")
	require.NoError(t, err)
	err = rdb.HSet(ctx, key, "name", "Alice")
	require.NoError(t, err)
	err = rdb.HSet(ctx, key, "role", "admin")
	require.NoError(t, err)

	// HGetAll should return all decrypted values
	result, err := rdb.HGetAll(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", result["email"])
	assert.Equal(t, "Alice", result["name"])
	assert.Equal(t, "admin", result["role"])
}

// TestHGetAll_MixedValues verifies HGetAll handles mixed encrypted and plaintext fields.
func TestHGetAll_MixedValues(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key := "session:user-uuid:session-uuid"

	// Write one field encrypted through RedisDB
	err = rdb.HSet(ctx, key, "email", "alice@example.com")
	require.NoError(t, err)

	// Write another field directly as plaintext (simulating pre-existing data)
	mr.HSet(key, "legacy_field", "plain_value")

	// HGetAll should handle both
	result, err := rdb.HGetAll(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", result["email"])
	assert.Equal(t, "plain_value", result["legacy_field"])
}

// TestGet_DecryptsWithPreviousKey verifies key rotation: values encrypted with the previous key
// can be decrypted when a new current key is in use.
func TestGet_DecryptsWithPreviousKey(t *testing.T) {
	rdb, _ := setupTestRedisDB(t)
	ctx := context.Background()

	oldKey := testKey(t)
	newKey := testKey(t)

	// First, encrypt with the old key
	oldEnc, err := crypto.NewSettingsEncryptorFromKeys(oldKey, nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(oldEnc)

	key := "cache:user:uuid-rotation"
	plaintext := `{"email":"rotate@example.com"}`

	err = rdb.Set(ctx, key, plaintext, time.Minute)
	require.NoError(t, err)

	// Now switch to new key with old key as previous
	newEnc, err := crypto.NewSettingsEncryptorFromKeys(newKey, oldKey, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(newEnc)

	// Should still be able to read the value (decrypts using previous key fallback)
	result, err := rdb.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, result)
}

// TestSet_OversizedValueSkipsEncryption verifies that values exceeding the encryption
// size limit are stored unencrypted with a warning (not an error).
func TestSet_OversizedValueSkipsEncryption(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key := "cache:user:uuid-oversized"
	// Create a value that will exceed 4000 chars when encrypted
	// Encrypted overhead: ~80 chars for ENC:v1:1:timestamp: prefix + base64 expansion (~4/3)
	// So a ~2950 char plaintext should exceed 4000 when encrypted
	plaintext := strings.Repeat("x", 3000)

	// Set should succeed (stores unencrypted due to size limit)
	err = rdb.Set(ctx, key, plaintext, time.Minute)
	require.NoError(t, err)

	// Raw value should be plaintext (no ENC: prefix)
	rawValue, err := mr.Get(key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, rawValue, "oversized value should be stored unencrypted")

	// Get should return it as-is
	result, err := rdb.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, result)
}

// TestGet_DecryptionFailureReturnsError verifies that corrupted encrypted values produce errors.
func TestGet_DecryptionFailureReturnsError(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key := "cache:user:uuid-corrupt"
	// Write a corrupted encrypted value directly
	require.NoError(t, mr.Set(key, "ENC:v1:1:1707431765:Y29ycnVwdGVkZGF0YQ=="))

	_, err = rdb.Get(ctx, key)
	assert.Error(t, err, "corrupted encrypted value should produce an error")
	assert.Contains(t, err.Error(), "failed to decrypt")
}

// TestHGet_DecryptionFailureReturnsError verifies that corrupted hash values produce errors.
func TestHGet_DecryptionFailureReturnsError(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	key := "session:user:session-corrupt"
	mr.HSet(key, "data", "ENC:v1:1:1707431765:Y29ycnVwdGVkZGF0YQ==")

	_, err = rdb.HGet(ctx, key, "data")
	assert.Error(t, err, "corrupted encrypted hash value should produce an error")
	assert.Contains(t, err.Error(), "failed to decrypt")
}

// TestMultipleSensitivePrefixes verifies encryption across all sensitive key prefix categories.
func TestMultipleSensitivePrefixes(t *testing.T) {
	rdb, mr := setupTestRedisDB(t)
	ctx := context.Background()

	enc, err := crypto.NewSettingsEncryptorFromKeys(testKey(t), nil, 1)
	require.NoError(t, err)
	rdb.SetEncryptor(enc)

	testCases := []struct {
		key   string
		value string
	}{
		{"cache:user:uuid1", `{"email":"a@b.com"}`},
		{"refresh_token:token-uuid", "user-uuid"},
		{"user_groups:a@b.com", `{"groups":["admin"]}`},
		{"tmi:settings:smtp.host", "mail.example.com"},
		{"oauth_state:state123", `{"provider":"google"}`},
		{"pkce:code123", `{"challenge":"abc"}`},
		{"user_deletion_challenge:a@b.com", "challenge-token"},
		{"session:user:sess", `{"active":true}`},
		{"auth:state:state456", `{"callback":"http://localhost"}`},
		{"auth:refresh:refresh456", "user-uuid-2"},
	}

	for _, tc := range testCases {
		t.Run(tc.key, func(t *testing.T) {
			err := rdb.Set(ctx, tc.key, tc.value, time.Minute)
			require.NoError(t, err)

			// Raw value should be encrypted
			rawValue, err := mr.Get(tc.key)
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(rawValue, "ENC:v1:"),
				"key %s: raw value should be encrypted, got: %s", tc.key, rawValue[:min(50, len(rawValue))])

			// Read back should return plaintext
			result, err := rdb.Get(ctx, tc.key)
			require.NoError(t, err)
			assert.Equal(t, tc.value, result, "key %s: decrypted value mismatch", tc.key)
		})
	}
}
